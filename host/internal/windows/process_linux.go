package windows

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"golang.org/x/sys/unix"
)

type processRunner struct{ privateBinDir string }
type processOutput struct {
	line ports.ProcessLine
	err  error
}

func executableName(role ports.ExecutableRole) (string, error) {
	switch role {
	case ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe:
		return string(role), nil
	}
	return "", errors.New("unknown executable role")
}

func privateExecutable(path string) (*os.File, error) {
	fd, err := openPath(path, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	if err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Mode&0022 != 0 || st.Mode&0100 == 0 {
		unix.Close(fd)
		if err == nil {
			err = errors.New("executable must be a regular user-owned executable without group/other writes")
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func checkPrivateBin(path string) error {
	fd, err := openPath(path, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 {
		return errors.New("executable directory must be user-owned and private (0700)")
	}
	return nil
}

func NewRunner(privateBinDir string) (ports.ProcessRunner, error) {
	if err := checkPrivateBin(privateBinDir); err != nil {
		return nil, err
	}
	for _, role := range []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe} {
		name, _ := executableName(role)
		f, err := privateExecutable(filepath.Join(privateBinDir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("private dependency %s: %w", role, err)
		}
		f.Close()
	}
	return &processRunner{privateBinDir: filepath.Clean(privateBinDir)}, nil
}

func unconfirmedProcessExit() error {
	return &contracts.ProtocolError{Code: contracts.CancelFailed, Message: "Owned process group exit could not be confirmed.", Retryable: true}
}

// Run directly execs a pinned private executable (including packaged scripts).
// A fresh process group owns ordinary descendants; cancellation signals only
// that group. This is not a sandbox: a hostile child can escape with setsid.
// Pdeathsig protects the direct child on host death, not escaped descendants.
// Keep callbacks short: callbacks are serialized on the calling goroutine.
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
	if err = checkPrivateBin(r.privateBinDir); err != nil {
		return result, err
	}
	executable := filepath.Join(r.privateBinDir, name)
	file, err := privateExecutable(executable)
	if err != nil {
		return result, err
	}
	defer file.Close()
	workDir := spec.Dir
	if workDir == "" {
		workDir = r.privateBinDir
	}
	dirFD, err := openPath(workDir, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return result, err
	}
	defer unix.Close(dirFD)
	for _, arg := range spec.Args {
		if strings.ContainsRune(arg, 0) {
			return result, errors.New("process argument contains NUL")
		}
	}
	if spec.Env != nil {
		seen := map[string]bool{}
		for _, entry := range spec.Env {
			key, _, ok := strings.Cut(entry, "=")
			if !ok || key == "" || seen[key] || strings.ContainsRune(entry, 0) {
				return result, errors.New("invalid explicit process environment")
			}
			seen[key] = true
		}
	}
	cmd := exec.Command("/proc/self/fd/3", spec.Args...)
	cmd.Args[0] = executable
	cmd.ExtraFiles = []*os.File{file}
	// /proc/<host>/fd avoids relying on Go's child chdir/fd-shuffle order.
	cmd.Dir = fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), dirFD)
	cmd.Env = spec.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	stdout, outChild, err := os.Pipe()
	if err != nil {
		return result, err
	}
	defer stdout.Close()
	defer outChild.Close()
	stderr, errChild, err := os.Pipe()
	if err != nil {
		return result, err
	}
	defer stderr.Close()
	defer errChild.Close()
	cmd.Stdout, cmd.Stderr = outChild, errChild
	if err = cmd.Start(); err != nil {
		return result, err
	}
	outChild.Close()
	errChild.Close()
	pid := cmd.Process.Pid
	waited := false
	defer func() {
		if !waited {
			_ = unix.Kill(-pid, unix.SIGKILL)
			// An uninterruptible child may outlive the bounded confirmation period.
			// Reap it when the kernel eventually permits exit; do not claim canceled.
			go func() { _ = cmd.Wait() }()
		}
	}()
	outputs := make(chan processOutput, 32)
	stop := make(chan struct{})
	defer close(stop)
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); readProcessLines(stdout, ports.Stdout, outputs, stop) }()
	go func() { defer readers.Done(); readProcessLines(stderr, ports.Stderr, outputs, stop) }()
	go func() { readers.Wait(); close(outputs) }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	done := ctx.Done()
	var runErr error
	var terminating, rootExited, streamsClosed, groupExited bool
	var deadline time.Time
	terminate := func() error {
		if !terminating {
			terminating = true
			deadline = time.Now().Add(5 * time.Second)
		}
		err := unix.Kill(-pid, unix.SIGKILL)
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		return err
	}
	for !rootExited || !groupExited || !streamsClosed {
		select {
		case <-done:
			done = nil
			runErr = ctx.Err()
			if err = terminate(); err != nil {
				return result, unconfirmedProcessExit()
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
				if err = terminate(); err != nil {
					return result, unconfirmedProcessExit()
				}
			} else if callback != nil {
				callback(output.line)
			}
		case <-ticker.C:
			if !rootExited {
				var info unix.Siginfo
				// WNOWAIT retains the child's PID until group cleanup completes,
				// preventing process-group-ID reuse while kill(-pid) is possible.
				err = unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
				if err != nil {
					return result, unconfirmedProcessExit()
				}
				rootExited = info.Code != 0
			}
			if rootExited || terminating {
				if err = terminate(); err != nil {
					return result, unconfirmedProcessExit()
				}
				groupExited, err = processGroupExited(pid)
				if err != nil {
					return result, unconfirmedProcessExit()
				}
				if time.Now().After(deadline) && (!rootExited || !groupExited || !streamsClosed) {
					return result, unconfirmedProcessExit()
				}
			}
		}
	}
	waitErr := cmd.Wait()
	waited = true
	result.ExitCode = cmd.ProcessState.ExitCode()
	if runErr != nil {
		result.Canceled = errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)
		return result, runErr
	}
	if waitErr != nil {
		return result, waitErr
	}
	return result, nil
}

// Zombies have exited and cannot hold output files open. Only the direct child
// is ours to wait/reap; orphan descendants are reaped by their system parent.
func processGroupExited(pgid int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return false, errors.New("invalid process status")
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 {
			return false, errors.New("invalid process status")
		}
		group, err := strconv.Atoi(fields[2])
		if err != nil {
			return false, err
		}
		if group == pgid && fields[0] != "Z" && fields[0] != "X" {
			return false, nil
		}
	}
	return true, nil
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
			if len(line) > ports.MaxProcessLineBytes || !utf8.Valid(line) {
				send(processOutput{err: errors.New("invalid or oversized process output line")})
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
