package windows

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	win "golang.org/x/sys/windows"
)

type processRunner struct {
	privateBinDir string
	queryExitCode func(win.Handle, *uint32) error
}

// Windows SDK: ProcThreadAttributeJobList (13) | PROC_THREAD_ATTRIBUTE_INPUT.
// x/sys v0.38.0 omits this constant. Supported from Windows 10; the bundled
// Deno dependency already requires Windows 10 1709 or newer.
const procThreadAttributeJobList = 0x0002000d

func executableName(role ports.ExecutableRole) (string, error) {
	switch role {
	case ports.YTDLP:
		return "yt-dlp.exe", nil
	case ports.Deno:
		return "deno.exe", nil
	case ports.FFmpeg:
		return "ffmpeg.exe", nil
	case ports.FFprobe:
		return "ffprobe.exe", nil
	default:
		return "", errors.New("unknown executable role")
	}
}

// NewRunner accepts a host-owned private directory, never executable paths or
// role names supplied by Native Messaging. Dependency hashes are verified by
// packaging; this boundary rechecks and pins local non-reparse paths on Run.
func NewRunner(privateBinDir string) (ports.ProcessRunner, error) {
	if err := CheckPath(privateBinDir, false); err != nil {
		return nil, fmt.Errorf("private executable directory: %w", err)
	}
	info, err := os.Stat(privateBinDir)
	if err != nil || !info.IsDir() {
		return nil, errors.New("private executable directory is unavailable")
	}
	for _, role := range []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe} {
		name, _ := executableName(role)
		path := filepath.Join(privateBinDir, name)
		if err := CheckPath(path, false); err != nil {
			return nil, fmt.Errorf("private dependency %s: %w", role, err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("private dependency %s is unavailable", role)
		}
	}
	return &processRunner{privateBinDir: filepath.Clean(privateBinDir), queryExitCode: win.GetExitCodeProcess}, nil
}

type processOutput struct {
	line ports.ProcessLine
	err  error
}

// Windows' JOBOBJECT_BASIC_ACCOUNTING_INFORMATION layout, including the active
// process count for all descendants. No process is killed by image name or PID.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}

func readJobAccounting(job win.Handle) (jobAccounting, error) {
	var accounting jobAccounting
	err := win.QueryInformationJobObject(job, win.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	return accounting, err
}

func jobActiveProcesses(job win.Handle) (uint32, error) {
	accounting, err := readJobAccounting(job)
	return accounting.ActiveProcesses, err
}

const maxTrackedJobProcesses = 512

type ownedProcessIdentity struct {
	PID     uint32
	Created win.Filetime
}

type jobProcessTracker struct {
	job     win.Handle
	handles map[ownedProcessIdentity]win.Handle
}

var isProcessInJobProc = win.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func processBelongsToJob(process, job win.Handle) (bool, error) {
	var member int32
	ok, _, err := isProcessInJobProc.Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&member)))
	if ok == 0 {
		return false, err
	}
	return member != 0, nil
}

func newJobProcessTracker(job, leader win.Handle) (*jobProcessTracker, error) {
	tracker := &jobProcessTracker{job: job, handles: make(map[ownedProcessIdentity]win.Handle)}
	var retained win.Handle
	if err := win.DuplicateHandle(win.CurrentProcess(), leader, win.CurrentProcess(), &retained, 0, false, win.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, unconfirmedProcessExit()
	}
	if err := tracker.retain(retained); err != nil {
		tracker.close()
		return nil, err
	}
	return tracker, nil
}

// Retained handles, plus creation times, preserve process identity even after
// PID reuse. Membership is verified before a handle is stored or ever waited.
// retain always takes ownership of the incoming handle, including on errors.
func (tracker *jobProcessTracker) retain(handle win.Handle) error {
	member, err := processBelongsToJob(handle, tracker.job)
	if err != nil || !member {
		win.CloseHandle(handle)
		return unconfirmedProcessExit()
	}
	pid, err := win.GetProcessId(handle)
	if err != nil {
		win.CloseHandle(handle)
		return unconfirmedProcessExit()
	}
	var created, exited, kernel, user win.Filetime
	if err := win.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		win.CloseHandle(handle)
		return unconfirmedProcessExit()
	}
	identity := ownedProcessIdentity{PID: pid, Created: created}
	if _, seen := tracker.handles[identity]; seen {
		win.CloseHandle(handle)
		return nil
	}
	if len(tracker.handles) >= maxTrackedJobProcesses {
		win.CloseHandle(handle)
		return unconfirmedProcessExit()
	}
	tracker.handles[identity] = handle
	return nil
}

func (tracker *jobProcessTracker) capture() error {
	capacity := uint32(16)
	for attempt := 0; attempt < 4; attempt++ {
		// The documented header is two DWORDs followed by ULONG_PTR IDs.
		buffer := make([]byte, 8+int(capacity)*int(unsafe.Sizeof(uintptr(0))))
		err := win.QueryInformationJobObject(tracker.job, win.JobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&buffer[0])), uint32(len(buffer)), nil)
		assigned := *(*uint32)(unsafe.Pointer(&buffer[0]))
		count := *(*uint32)(unsafe.Pointer(&buffer[4]))
		if errors.Is(err, win.ERROR_MORE_DATA) || (err == nil && count < assigned) {
			if assigned > maxTrackedJobProcesses {
				return unconfirmedProcessExit()
			}
			capacity = assigned
			if capacity < 16 {
				capacity = 16
			}
			continue
		}
		if err != nil || count > capacity || assigned > maxTrackedJobProcesses {
			return unconfirmedProcessExit()
		}
		pids := unsafe.Slice((*uintptr)(unsafe.Pointer(&buffer[8])), int(count))
		for _, pid := range pids {
			if pid == 0 || pid > uintptr(^uint32(0)) {
				return unconfirmedProcessExit()
			}
			handle, err := win.OpenProcess(win.SYNCHRONIZE|win.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
			if err != nil {
				return unconfirmedProcessExit()
			}
			if err := tracker.retain(handle); err != nil {
				return err
			}
		}
		return nil
	}
	return unconfirmedProcessExit()
}

func (tracker *jobProcessTracker) exited(accounting jobAccounting) (bool, error) {
	if accounting.ActiveProcesses != 0 {
		return false, nil
	}
	// A rapid unobserved birth, an unopenable process, or the tracking limit
	// prevents confirmation. Never substitute accounting for physical waits.
	if accounting.TotalProcesses != uint32(len(tracker.handles)) {
		return false, unconfirmedProcessExit()
	}
	for _, handle := range tracker.handles {
		status, err := win.WaitForSingleObject(handle, 0)
		if err != nil {
			return false, unconfirmedProcessExit()
		}
		if status != win.WAIT_OBJECT_0 {
			return false, nil
		}
	}
	return true, nil
}

func (tracker *jobProcessTracker) close() {
	for identity, handle := range tracker.handles {
		win.CloseHandle(handle)
		delete(tracker.handles, identity)
	}
}

func unconfirmedProcessExit() error {
	return &contracts.ProtocolError{Code: contracts.CancelFailed,
		Message: "Owned process tree exit could not be confirmed.", Retryable: true}
}

func confirmProcessTreeExit(job, process win.Handle, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := win.WaitForSingleObject(process, 0)
		if err != nil {
			return unconfirmedProcessExit()
		}
		active, err := jobActiveProcesses(job)
		if err != nil {
			return unconfirmedProcessExit()
		}
		if status == win.WAIT_OBJECT_0 && active == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return unconfirmedProcessExit()
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func createProcessJob() (win.Handle, error) {
	sd, err := privateSecurity()
	if err != nil {
		return 0, err
	}
	// A non-inheritable, unnamed handle is owned only by the host. Closing the
	// host, even abruptly, closes its last reference and kills the entire job.
	sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), SecurityDescriptor: sd}
	job, err := win.CreateJobObject(&sa, nil)
	if err != nil {
		return 0, err
	}
	limits := win.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = win.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	// BREAKAWAY_OK and SILENT_BREAKAWAY_OK are deliberately absent.
	if _, err := win.SetInformationJobObject(job, win.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		win.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// Run owns one job, its pipes, and all descendants for its full lifetime. Child
// execution begins only after assignment succeeds. Callbacks are serialized;
// callers should return promptly so progress delivery cannot block cancellation.
func (r *processRunner) Run(ctx context.Context, spec ports.ExecSpec, callback func(ports.ProcessLine)) (ports.ProcessResult, error) {
	result := ports.ProcessResult{ExitCode: -1}
	if ctx == nil {
		return result, errors.New("process context is required")
	}
	if err := ctx.Err(); err != nil {
		result.Canceled = true
		return result, err
	}
	name, err := executableName(spec.Executable)
	if err != nil {
		return result, err
	}
	executable := filepath.Join(r.privateBinDir, name)
	unlockExecutable, err := LockPath(executable, false)
	if err != nil {
		return result, fmt.Errorf("private executable path: %w", err)
	}
	defer unlockExecutable()
	// Version/capability probes do not need a task directory. Their omitted Dir
	// means this fixed host-owned location; a supplied Dir still must be absolute.
	workDir := spec.Dir
	if workDir == "" {
		workDir = r.privateBinDir
	}
	unlockDir, err := LockPath(workDir, false)
	if err != nil {
		return result, fmt.Errorf("process working directory: %w", err)
	}
	defer unlockDir()
	info, err := os.Stat(workDir)
	if err != nil || !info.IsDir() {
		return result, errors.New("process working directory is unavailable")
	}
	appName, err := win.UTF16PtrFromString(executable)
	if err != nil {
		return result, err
	}
	// Denying write/delete sharing pins the executable contents during startup
	// in addition to LockPath pinning every ancestor against replacement.
	exeHandle, err := win.CreateFile(appName, win.GENERIC_READ, win.FILE_SHARE_READ, nil,
		win.OPEN_EXISTING, win.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return result, fmt.Errorf("private executable unavailable: %w", err)
	}
	defer win.CloseHandle(exeHandle)
	var exeInfo win.ByHandleFileInformation
	if err := win.GetFileInformationByHandle(exeHandle, &exeInfo); err != nil {
		return result, err
	}
	if exeInfo.FileAttributes&(win.FILE_ATTRIBUTE_DIRECTORY|win.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return result, errors.New("private executable is not a regular local file")
	}
	args := append([]string{executable}, spec.Args...)
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return result, errors.New("process argument contains NUL")
		}
	}
	commandLine, err := win.UTF16FromString(win.ComposeCommandLine(args))
	if err != nil {
		return result, err
	}
	if len(commandLine) > 32767 {
		return result, errors.New("process command line exceeds Windows limit")
	}
	currentDir, err := win.UTF16PtrFromString(workDir)
	if err != nil {
		return result, err
	}
	job, err := createProcessJob()
	if err != nil {
		return result, fmt.Errorf("create owned process job: %w", err)
	}
	defer func() {
		if job != 0 {
			win.CloseHandle(job)
		}
	}()
	stdout, stdoutChild, err := childOutputPipe()
	if err != nil {
		return result, err
	}
	defer stdout.Close()
	defer stdoutChild.Close()
	stderr, stderrChild, err := childOutputPipe()
	if err != nil {
		return result, err
	}
	defer stderr.Close()
	defer stderrChild.Close()
	nullName, _ := win.UTF16PtrFromString("NUL")
	sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), InheritHandle: 1}
	stdin, err := win.CreateFile(nullName, win.GENERIC_READ, win.FILE_SHARE_READ|win.FILE_SHARE_WRITE,
		&sa, win.OPEN_EXISTING, 0, 0)
	if err != nil {
		return result, err
	}
	defer func() {
		if stdin != 0 {
			win.CloseHandle(stdin)
		}
	}()
	inherited := []win.Handle{stdin, win.Handle(stdoutChild.Fd()), win.Handle(stderrChild.Fd())}
	if err := ctx.Err(); err != nil {
		result.Canceled = true
		return result, err
	}
	process, err := createSuspendedProcessWithEnv(job, appName, commandLine, currentDir, inherited, spec.Env)
	if err != nil {
		return result, fmt.Errorf("create private process: %w", err)
	}
	defer win.CloseHandle(process.Process)
	defer win.CloseHandle(process.Thread)
	tracker, err := newJobProcessTracker(job, process.Process)
	if err != nil {
		return result, unconfirmedProcessExit()
	}
	defer tracker.close()
	if _, err := win.ResumeThread(process.Thread); err != nil {
		if win.TerminateJobObject(job, 1) != nil || confirmProcessTreeExit(job, process.Process, 10*time.Second) != nil {
			return result, unconfirmedProcessExit()
		}
		return result, fmt.Errorf("resume owned process: %w", err)
	}
	// Drop parent references to child stdio now, so EOF is physical tree exit.
	stdoutChild.Close()
	stderrChild.Close()
	win.CloseHandle(stdin)
	stdin = 0
	// On an error, close the job before closing pipes: any blocked reads then
	// receive physical EOF from the killed tree. The earlier defer covers setup.
	defer func() { win.CloseHandle(job); job = 0 }()
	outputs := make(chan processOutput, 32)
	stopReaders := make(chan struct{})
	defer close(stopReaders)
	var readers sync.WaitGroup
	readers.Add(2)
	for _, stream := range []struct {
		file *os.File
		kind ports.ProcessStream
	}{{stdout, ports.Stdout}, {stderr, ports.Stderr}} {
		go func(file *os.File, kind ports.ProcessStream) {
			defer readers.Done()
			readProcessLines(file, kind, outputs, stopReaders)
		}(stream.file, stream.kind)
	}
	go func() { readers.Wait(); close(outputs) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ctxDone := ctx.Done()
	var runErr error
	var terminating, rootExited, treeExited, streamsClosed, canceled bool
	var terminateDeadline time.Time
	terminate := func() error {
		if terminating {
			return nil
		}
		if err := tracker.capture(); err != nil {
			return err
		}
		terminating = true
		terminateDeadline = time.Now().Add(10 * time.Second)
		return win.TerminateJobObject(job, 1)
	}
	for !rootExited || !treeExited || !streamsClosed {
		select {
		case <-ctxDone:
			ctxDone = nil
			if !rootExited || !treeExited {
				canceled = true
				runErr = ctx.Err()
				if err := terminate(); err != nil {
					return result, unconfirmedProcessExit()
				}
			}
		case output, ok := <-outputs:
			if !ok {
				streamsClosed = true
				outputs = nil
				continue
			}
			if output.err != nil {
				if runErr == nil {
					runErr = output.err
				}
				if err := terminate(); err != nil {
					return result, unconfirmedProcessExit()
				}
			} else if callback != nil {
				callback(output.line)
			}
		case <-ticker.C:
			if err := tracker.capture(); err != nil {
				return result, unconfirmedProcessExit()
			}
			if !rootExited {
				status, err := win.WaitForSingleObject(process.Process, 0)
				if err != nil {
					return result, unconfirmedProcessExit()
				}
				if status == win.WAIT_OBJECT_0 {
					rootExited = true
					var code uint32
					if err := r.queryExitCode(process.Process, &code); err != nil {
						return result, unconfirmedProcessExit()
					}
					result.ExitCode = int(code)
				}
			}
			if rootExited || terminating {
				accounting, err := readJobAccounting(job)
				if err != nil {
					return result, unconfirmedProcessExit()
				}
				treeExited, err = tracker.exited(accounting)
				if err != nil {
					return result, unconfirmedProcessExit()
				}
				// A crashed/exited root cannot leave orphaned owned descendants.
				if rootExited && !treeExited {
					if err := terminate(); err != nil {
						return result, unconfirmedProcessExit()
					}
				}
			}
			if terminating && !treeExited && time.Now().After(terminateDeadline) {
				return result, unconfirmedProcessExit()
			}
		}
	}
	// This is the only post-launch path that can report Canceled: both the root
	// process handles and the complete job accounting confirmed exit, and pipes drained.
	result.Canceled = canceled
	if runErr != nil {
		return result, runErr
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("process exited with code %d", result.ExitCode)
	}
	return result, nil
}

func createSuspendedProcess(job win.Handle, appName *uint16, commandLine []uint16, currentDir *uint16, inherited []win.Handle) (win.ProcessInformation, error) {
	return createSuspendedProcessWithEnv(job, appName, commandLine, currentDir, inherited, nil)
}

func createSuspendedProcessWithEnv(job win.Handle, appName *uint16, commandLine []uint16, currentDir *uint16, inherited []win.Handle, environment []string) (win.ProcessInformation, error) {
	var process win.ProcessInformation
	attributes, err := win.NewProcThreadAttributeList(2)
	if err != nil {
		return process, err
	}
	defer attributes.Delete()
	if err := attributes.Update(win.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inherited[0]),
		uintptr(len(inherited))*unsafe.Sizeof(inherited[0])); err != nil {
		return process, err
	}
	// Association is atomic with process creation, closing the host-exit window
	// between CreateProcess and a separate AssignProcessToJobObject call. The
	// job handle is an attribute input, not an inherited controlling handle.
	jobs := []win.Handle{job}
	if err := attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs[0])); err != nil {
		return process, err
	}
	startup := win.StartupInfoEx{StartupInfo: win.StartupInfo{
		Flags: win.STARTF_USESTDHANDLES, StdInput: inherited[0], StdOutput: inherited[1], StdErr: inherited[2],
	}, ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	// The explicit application name prevents PATH/current-directory resolution.
	// HANDLE_LIST prevents inheriting any handle besides the three stdio handles.
	flags := uint32(win.CREATE_SUSPENDED | win.CREATE_NO_WINDOW | win.EXTENDED_STARTUPINFO_PRESENT)
	var envPointer *uint16
	var envBlock []uint16
	if environment != nil {
		seen := map[string]bool{}
		ordered := append([]string(nil), environment...)
		sort.Slice(ordered, func(i, j int) bool { return strings.ToUpper(ordered[i]) < strings.ToUpper(ordered[j]) })
		for _, entry := range ordered {
			key, _, ok := strings.Cut(entry, "=")
			if !ok || key == "" || strings.ContainsRune(entry, 0) || seen[strings.ToUpper(key)] {
				return process, errors.New("invalid explicit process environment")
			}
			seen[strings.ToUpper(key)] = true
			value, envErr := win.UTF16FromString(entry)
			if envErr != nil {
				return process, envErr
			}
			envBlock = append(envBlock, value...)
		}
		envBlock = append(envBlock, 0)
		if len(envBlock) == 1 {
			envBlock = append(envBlock, 0)
		}
		envPointer = &envBlock[0]
		flags |= win.CREATE_UNICODE_ENVIRONMENT
	}
	err = win.CreateProcess(appName, &commandLine[0], nil, nil, true, flags, envPointer, currentDir, &startup.StartupInfo, &process)
	return process, err
}

func childOutputPipe() (*os.File, *os.File, error) {
	sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), InheritHandle: 1}
	var read, write win.Handle
	if err := win.CreatePipe(&read, &write, &sa, 0); err != nil {
		return nil, nil, err
	}
	if err := win.SetHandleInformation(read, win.HANDLE_FLAG_INHERIT, 0); err != nil {
		win.CloseHandle(read)
		win.CloseHandle(write)
		return nil, nil, err
	}
	return os.NewFile(uintptr(read), "process-output"), os.NewFile(uintptr(write), "child-output"), nil
}

func readProcessLines(file *os.File, stream ports.ProcessStream, outputs chan<- processOutput, stop <-chan struct{}) {
	reader := bufio.NewReaderSize(file, 4096)
	line := make([]byte, 0, 4096)
	send := func(value processOutput) bool {
		select {
		case outputs <- value:
			return true
		case <-stop:
			return false
		}
	}
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > ports.MaxProcessLineBytes+2 {
			send(processOutput{err: errors.New("process output line exceeds limit")})
			return
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(line) != 0 {
			line = []byte(strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"))
			if len(line) > ports.MaxProcessLineBytes {
				send(processOutput{err: errors.New("process output line exceeds limit")})
				return
			}
			if !utf8.Valid(line) {
				send(processOutput{err: errors.New("process output is not UTF-8")})
				return
			}
			if !send(processOutput{line: ports.ProcessLine{Stream: stream, Text: string(line)}}) {
				return
			}
		}
		line = line[:0]
		if err != nil {
			if !errors.Is(err, io.EOF) {
				send(processOutput{err: errors.New("read process output failed")})
			}
			return
		}
	}
}
