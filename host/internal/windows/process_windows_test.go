package windows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	win "golang.org/x/sys/windows"
)

// A dedicated source-built fixture exercises Windows process behavior without
// copying or invoking the Go test executable itself.
func fixtureArgs(mode string, args ...string) []string {
	return append([]string{mode}, args...)
}

var fixtureBuildOnce sync.Once
var fixtureBuiltPath string
var fixtureBuildErr error

func buildProcessFixture() {
	_, source, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	fixtureBuiltPath = filepath.Join(repo, ".tools", "integration", "processfixture.exe")
	if err := os.MkdirAll(filepath.Dir(fixtureBuiltPath), 0700); err != nil {
		fixtureBuildErr = err
		return
	}
	cmd := exec.Command(filepath.Join(repo, ".tools", "go", "bin", "go.exe"), "-C", filepath.Join(repo, "host"), "build", "-o", fixtureBuiltPath, "./internal/windows/testdata/processfixture")
	settings := map[string]string{
		"GOTOOLCHAIN": "local", "GOCACHE": filepath.Join(repo, ".tools", "gocache"),
		"GOMODCACHE": filepath.Join(repo, ".tools", "gomodcache"), "GOTMPDIR": filepath.Join(repo, ".tools", "tmp-process"),
	}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, replace := settings[strings.ToUpper(key)]; !replace {
			cmd.Env = append(cmd.Env, value)
		}
	}
	for key, value := range settings {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		fixtureBuildErr = fmt.Errorf("build process fixture: %w: %s", err, output)
	}
}

func fixtureBin(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private bin")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixtureBuildOnce.Do(buildProcessFixture)
	if fixtureBuildErr != nil {
		t.Fatal(fixtureBuildErr)
	}
	exe := fixtureBuiltPath
	for _, name := range []string{"yt-dlp.exe", "deno.exe", "ffmpeg.exe", "ffprobe.exe"} {
		in, err := os.Open(exe)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			in.Close()
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	return dir
}

func TestRunnerExplicitEnvironmentDoesNotInheritDenoPermissionBroker(t *testing.T) {
	t.Setenv("DENO_PERMISSION_BROKER_PATH", "inherited-untrusted-broker")
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	var output string
	result, err := runner.Run(context.Background(), ports.ExecSpec{Executable: ports.YTDLP, Dir: bin, Args: fixtureArgs("env", "DENO_PERMISSION_BROKER_PATH", "DWNL_FIXTURE_VALUE"), Env: []string{"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "DWNL_FIXTURE_VALUE=ação-東京"}}, func(line ports.ProcessLine) {
		if line.Stream == ports.Stdout {
			output = line.Text
		}
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("run %+v %v", result, err)
	}
	var values map[string]string
	if err = json.Unmarshal([]byte(output), &values); err != nil {
		t.Fatal(err)
	}
	if values["DENO_PERMISSION_BROKER_PATH"] != "" || values["DWNL_FIXTURE_VALUE"] != "ação-東京" {
		t.Fatalf("inherited or corrupted environment: %+v", values)
	}
}

func TestRunnerRunsFixedRoleWithLiteralArgumentsAndBothStreams(t *testing.T) {
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"", "has spaces", `quote\"value`, `ends in slash\`, "& echo literal > ignored.txt", "日本語"}
	var lines []ports.ProcessLine
	result, err := runner.Run(context.Background(), ports.ExecSpec{
		Executable: ports.FFmpeg, Args: fixtureArgs("echo", wantArgs...), Dir: bin,
	}, func(line ports.ProcessLine) { lines = append(lines, line) })
	if err != nil || result.ExitCode != 0 || result.Canceled {
		t.Fatalf("run: %+v, %v", result, err)
	}
	var gotArgs []string
	var stderr bool
	for _, line := range lines {
		if line.Stream == ports.Stdout {
			if err := json.Unmarshal([]byte(line.Text), &gotArgs); err != nil {
				t.Fatal(err)
			}
		}
		if line.Stream == ports.Stderr && line.Text == "fixture stderr" {
			stderr = true
		}
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) || !stderr {
		t.Fatalf("args=%q, stderr=%v, lines=%+v", gotArgs, stderr, lines)
	}
	if _, err := os.Stat(filepath.Join(bin, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("shell-like argument had a side effect: %v", err)
	}
}

// A missing tree termination, premature Canceled, or name-based global kill
// makes this test fail. Handles are opened while every fixture is still alive.
func TestCancelKillsAllOwnedChildrenButNotOtherFfmpeg(t *testing.T) {
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	other := exec.Command(filepath.Join(bin, "ffmpeg.exe"), fixtureArgs("sleep")...)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Process.Kill(); other.Wait() })
	otherHandle := processHandle(t, uint32(other.Process.Pid))
	defer win.CloseHandle(otherHandle)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	treeReady := make(chan []uint32, 1)
	done := make(chan struct{})
	var result ports.ProcessResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = runner.Run(ctx, ports.ExecSpec{Executable: ports.YTDLP, Args: fixtureArgs("tree"), Dir: bin}, func(line ports.ProcessLine) {
			if line.Stream == ports.Stdout && strings.HasPrefix(line.Text, "tree:") {
				var pids []uint32
				if json.Unmarshal([]byte(strings.TrimPrefix(line.Text, "tree:")), &pids) == nil {
					treeReady <- pids
				}
			}
		})
	}()
	var pids []uint32
	select {
	case pids = <-treeReady:
	case <-done:
		t.Fatalf("run ended before creating a real tree: %+v, %v", result, runErr)
	case <-time.After(15 * time.Second):
		t.Fatal("tree did not become ready")
	}
	if len(pids) != 3 {
		t.Fatalf("expected root, child, grandchild: %v", pids)
	}
	var handles []win.Handle
	for _, pid := range pids {
		h := processHandle(t, pid)
		handles = append(handles, h)
		defer win.CloseHandle(h)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("cancellation did not complete")
	}
	if !result.Canceled || runErr != context.Canceled {
		t.Fatalf("cancel result=%+v, err=%v", result, runErr)
	}
	for i, h := range handles {
		status, err := win.WaitForSingleObject(h, 0)
		if err != nil || status != win.WAIT_OBJECT_0 {
			t.Fatalf("Canceled returned while owned PID %d remained alive: %d, %v", pids[i], status, err)
		}
	}
	status, err := win.WaitForSingleObject(otherHandle, 0)
	if err != nil || status != uint32(win.WAIT_TIMEOUT) {
		t.Fatalf("unrelated ffmpeg PID %d was stopped: %d, %v", other.Process.Pid, status, err)
	}
	t.Logf("owned root/child/grandchild %v signaled before Canceled=true; unrelated ffmpeg PID %d remained alive", pids, other.Process.Pid)
}

func processHandle(t *testing.T, pid uint32) win.Handle {
	t.Helper()
	h, err := win.OpenProcess(win.SYNCHRONIZE|win.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("open live PID %d: %v", pid, err)
	}
	return h
}

// Removing atomic job association leaves the suspended process outside the job
// when creation returns; a host dying at that point would leave it behind.
func TestSuspendedChildIsOwnedAtCreationAndKilledBeforeResume(t *testing.T) {
	bin := fixtureBin(t)
	job, err := createProcessJob()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if job != 0 {
			win.CloseHandle(job)
		}
	}()
	stdout, stdoutChild, err := childOutputPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	defer stdoutChild.Close()
	stderr, stderrChild, err := childOutputPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	defer stderrChild.Close()
	nullName, _ := win.UTF16PtrFromString("NUL")
	sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), InheritHandle: 1}
	stdin, err := win.CreateFile(nullName, win.GENERIC_READ, win.FILE_SHARE_READ|win.FILE_SHARE_WRITE, &sa, win.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(stdin)
	exe := filepath.Join(bin, "yt-dlp.exe")
	appName, _ := win.UTF16PtrFromString(exe)
	commandLine, _ := win.UTF16FromString(win.ComposeCommandLine(append([]string{exe}, fixtureArgs("echo")...)))
	currentDir, _ := win.UTF16PtrFromString(bin)
	process, err := createSuspendedProcess(job, appName, commandLine, currentDir, []win.Handle{stdin, win.Handle(stdoutChild.Fd()), win.Handle(stderrChild.Fd())})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		win.TerminateProcess(process.Process, 1)
		win.WaitForSingleObject(process.Process, 5000)
		win.CloseHandle(process.Thread)
		win.CloseHandle(process.Process)
	}()
	active, err := jobActiveProcesses(job)
	if err != nil || active != 1 {
		t.Fatalf("suspended child returned before job ownership: active=%d, err=%v", active, err)
	}
	if err := win.CloseHandle(job); err != nil {
		t.Fatal(err)
	}
	job = 0
	status, err := win.WaitForSingleObject(process.Process, 5000)
	if err != nil || status != win.WAIT_OBJECT_0 {
		t.Fatalf("closing job did not kill suspended child before resume: %d, %v", status, err)
	}
	t.Logf("PID %d was job-owned at CreateProcess return and signaled after job close without ResumeThread", process.ProcessId)
}

func TestRunnerRejectsUnknownRolesAndInvalidPaths(t *testing.T) {
	bin := fixtureBin(t)
	if _, err := NewRunner("relative"); err == nil {
		t.Fatal("relative executable root accepted")
	}
	if _, err := NewRunner(t.TempDir()); err == nil {
		t.Fatal("root without fixed dependencies accepted")
	}
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []ports.ExecSpec{
		{Executable: "cmd", Dir: bin},
		{Executable: ports.ExecutableRole(filepath.Join(bin, "ffmpeg.exe")), Dir: bin},
		{Executable: ports.FFmpeg, Dir: "relative"},
		{Executable: ports.FFmpeg, Dir: filepath.Join(bin, "absent")},
		{Executable: ports.FFmpeg, Dir: bin, Args: []string{"bad\x00arg"}},
	} {
		if _, err := runner.Run(context.Background(), spec, nil); err == nil {
			t.Fatalf("unsafe spec accepted: %+v", spec)
		}
	}
}

func TestRunnerDefaultsEmptyWorkingDirectoryToPrivateBin(t *testing.T) {
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	result, err := runner.Run(context.Background(), ports.ExecSpec{Executable: ports.FFprobe, Args: fixtureArgs("pwd")}, func(line ports.ProcessLine) {
		if line.Stream == ports.Stdout {
			got = line.Text
		}
	})
	if err != nil || result.ExitCode != 0 || result.Canceled || !strings.EqualFold(got, bin) {
		t.Fatalf("empty working directory did not use fixed private bin: result=%+v, err=%v, dir=%q, want=%q", result, err, got, bin)
	}
}

func TestRunnerDoesNotInheritUnrelatedHandlesOrAllowBreakaway(t *testing.T) {
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), InheritHandle: 1}
	event, err := win.CreateEvent(&sa, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(event)
	for _, tc := range []struct {
		mode string
		args []string
		want string
	}{
		{"handle", []string{strconv.FormatUint(uint64(event), 10)}, "unrelated handle absent"},
		{"breakaway", nil, "breakaway rejected"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			var got []string
			result, err := runner.Run(context.Background(), ports.ExecSpec{Executable: ports.Deno, Args: fixtureArgs(tc.mode, tc.args...), Dir: bin}, func(line ports.ProcessLine) {
				if line.Stream == ports.Stdout {
					got = append(got, line.Text)
				}
			})
			if err != nil || result.Canceled || result.ExitCode != 0 || !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("result=%+v, err=%v, lines=%q", result, err, got)
			}
		})
	}
	status, err := win.WaitForSingleObject(event, 0)
	if err != nil || status != uint32(win.WAIT_TIMEOUT) {
		t.Fatalf("child received unrelated event handle: %d, %v", status, err)
	}
}

func TestRunnerBoundsProcessLinesAndDrainsAfterOverflow(t *testing.T) {
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{65536, 65537} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var got []ports.ProcessLine
			result, err := runner.Run(ctx, ports.ExecSpec{Executable: ports.FFprobe, Args: fixtureArgs("line", strconv.Itoa(size)), Dir: bin}, func(line ports.ProcessLine) { got = append(got, line) })
			for _, line := range got {
				if len(line.Text) > 65536 {
					t.Fatalf("oversized callback %d", len(line.Text))
				}
			}
			if size == 65536 {
				if err != nil || result.ExitCode != 0 || len(got) != 1 || len(got[0].Text) != 65536 {
					t.Fatalf("boundary: result=%+v, err=%v, lines=%d", result, err, len(got))
				}
			} else if err == nil || result.Canceled || len(got) != 0 {
				t.Fatalf("overflow must fail without truncation: result=%+v, err=%v, lines=%d", result, err, len(got))
			}
		})
	}
}

func TestRunnerReportsExitCodeAndPreCanceledContext(t *testing.T) {
	bin := fixtureBin(t)
	runner, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), ports.ExecSpec{Executable: ports.FFmpeg, Args: fixtureArgs("exit", "7"), Dir: bin}, nil)
	if err == nil || result.ExitCode != 7 || result.Canceled {
		t.Fatalf("exit result=%+v, err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = runner.Run(ctx, ports.ExecSpec{Executable: ports.FFmpeg, Args: fixtureArgs("sleep"), Dir: bin}, nil)
	if err != context.Canceled || !result.Canceled {
		t.Fatalf("pre-cancel result=%+v, err=%v", result, err)
	}
}

// A generic error here can let the caller treat context cancellation as final
// even though the runner has not yet checked the complete job's active count.
func TestRunnerExitCodeQueryFailureReturnsCancelFailed(t *testing.T) {
	bin := fixtureBin(t)
	boundary, err := NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	runner := boundary.(*processRunner)
	// The seam is private and per-instance. Process creation, jobs, descendants,
	// cancellation, and handle waits remain actual Windows operations.
	runner.queryExitCode = func(win.Handle, *uint32) error { return win.ERROR_ACCESS_DENIED }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	treeReady := make(chan []uint32, 1)
	done := make(chan struct{})
	var result ports.ProcessResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = runner.Run(ctx, ports.ExecSpec{Executable: ports.YTDLP, Args: fixtureArgs("tree"), Dir: bin}, func(line ports.ProcessLine) {
			if line.Stream == ports.Stdout && strings.HasPrefix(line.Text, "tree:") {
				var pids []uint32
				if json.Unmarshal([]byte(strings.TrimPrefix(line.Text, "tree:")), &pids) == nil {
					treeReady <- pids
				}
			}
		})
	}()
	var pids []uint32
	select {
	case pids = <-treeReady:
	case <-done:
		t.Fatalf("tree ended before query-failure regression: result=%+v, err=%v", result, runErr)
	case <-time.After(15 * time.Second):
		t.Fatal("tree did not become ready")
	}
	if len(pids) != 3 {
		t.Fatalf("expected real root/child/grandchild: %v", pids)
	}
	var handles []win.Handle
	for _, pid := range pids {
		h := processHandle(t, pid)
		handles = append(handles, h)
		defer win.CloseHandle(h)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("query-failure run did not return")
	}
	// Wait for fixture cleanup even in the RED phase, without mutating any
	// process/control handle remotely or claiming that exit was confirmed by Run.
	for i, handle := range handles {
		status, err := win.WaitForSingleObject(handle, 5000)
		if err != nil || status != win.WAIT_OBJECT_0 {
			t.Fatalf("fixture PID %d did not exit: %d, %v", pids[i], status, err)
		}
	}
	var protocolErr *contracts.ProtocolError
	if result.Canceled || !errors.As(runErr, &protocolErr) || protocolErr.Code != contracts.CancelFailed {
		t.Fatalf("exit-code query failed before full tree confirmation: want CANCEL_FAILED/Canceled=false, result=%+v, err=%v", result, runErr)
	}
}

func TestAbruptHostExitKillsJobAndAllOwnedDescendants(t *testing.T) {
	bin := fixtureBin(t)
	exe := filepath.Join(bin, "yt-dlp.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	host := exec.CommandContext(ctx, exe, fixtureArgs("host", bin)...)
	pipe, err := host.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Process.Kill() })
	line := make(chan string, 1)
	go func() { var value string; fmt.Fscanln(pipe, &value); line <- value }()
	var pids []uint32
	select {
	case value := <-line:
		if !strings.HasPrefix(value, "tree:") {
			host.Process.Kill()
			host.Wait()
			t.Fatalf("host did not expose owned tree: %q", value)
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(value, "tree:")), &pids); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("host fixture did not start tree")
	}
	if len(pids) != 3 {
		t.Fatalf("owned tree=%v", pids)
	}
	var handles []win.Handle
	for _, pid := range pids {
		h := processHandle(t, pid)
		handles = append(handles, h)
		defer win.CloseHandle(h)
	}
	if err := host.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	host.Wait()
	for i, h := range handles {
		status, err := win.WaitForSingleObject(h, 5000)
		if err != nil || status != win.WAIT_OBJECT_0 {
			t.Fatalf("host died but owned PID %d remained alive: %d, %v", pids[i], status, err)
		}
	}
	t.Logf("abrupt host termination closed non-inherited controlling handle and killed owned PIDs %v", pids)
}
