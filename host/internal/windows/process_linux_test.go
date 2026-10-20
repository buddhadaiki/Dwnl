package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"dwnl/host/internal/ports"
)

func TestLinuxProcessHelper(t *testing.T) {
	mode := os.Getenv("DWNL_LINUX_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "echo":
		fmt.Println(strings.Join(os.Args[3:], "|"))
		os.Exit(0)
	case "oversize":
		fmt.Println(strings.Repeat("x", ports.MaxProcessLineBytes+1))
		os.Exit(0)
	case "tree", "orphan":
		path, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		child := exec.Command(path, "-test.run=^TestLinuxProcessHelper$")
		child.Env = []string{"DWNL_LINUX_HELPER=sleep"}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err = child.Start(); err != nil {
			os.Exit(3)
		}
		fmt.Printf("child=%d\n", child.Process.Pid)
		if mode == "orphan" {
			os.Exit(0)
		}
		for {
			time.Sleep(time.Second)
		}
	case "sleep":
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(4)
}

func linuxRunnerFixture(t *testing.T) ports.ProcessRunner {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "yt-dlp"), data, 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(dir)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestLinuxRunnerDirectArgumentsAndOutputLimit(t *testing.T) {
	runner := linuxRunnerFixture(t)
	var lines []string
	result, err := runner.Run(context.Background(), ports.ExecSpec{Executable: ports.YTDLP, Args: []string{"-test.run=^TestLinuxProcessHelper$", "--", "literal;$(false)"}, Env: []string{"DWNL_LINUX_HELPER=echo"}}, func(line ports.ProcessLine) { lines = append(lines, line.Text) })
	if err != nil || result.ExitCode != 0 || len(lines) != 1 || lines[0] != "literal;$(false)" {
		t.Fatalf("direct run: %+v %v %v", result, err, lines)
	}
	_, err = runner.Run(context.Background(), ports.ExecSpec{Executable: ports.YTDLP, Args: []string{"-test.run=^TestLinuxProcessHelper$"}, Env: []string{"DWNL_LINUX_HELPER=oversize"}}, nil)
	if err == nil {
		t.Fatal("accepted oversized output")
	}
}

func TestLinuxRunnerCancellationKillsGroupPreservesUnrelated(t *testing.T) {
	runner := linuxRunnerFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := exec.Command(executable, "-test.run=^TestLinuxProcessHelper$")
	unrelated.Env = []string{"DWNL_LINUX_HELPER=sleep"}
	if err = unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	childPID := 0
	result, err := runner.Run(ctx, ports.ExecSpec{Executable: ports.YTDLP, Args: []string{"-test.run=^TestLinuxProcessHelper$"}, Env: []string{"DWNL_LINUX_HELPER=tree"}}, func(line ports.ProcessLine) {
		if strings.HasPrefix(line.Text, "child=") {
			childPID, _ = strconv.Atoi(strings.TrimPrefix(line.Text, "child="))
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || !result.Canceled || childPID == 0 {
		t.Fatalf("cancel: %+v %v child=%d", result, err, childPID)
	}
	data, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID))
	if readErr == nil {
		end := strings.LastIndexByte(string(data), ')')
		fields := strings.Fields(string(data[end+1:]))
		if fields[0] != "Z" && fields[0] != "X" {
			t.Fatalf("child alive: %s", data)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if err = unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process affected: %v", err)
	}
}

func TestLinuxRunnerRejectsSymlinkAndAllowsPrivateScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ffmpeg")
	if err := os.Symlink("/usr/bin/true", path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunner(dir); err == nil {
		t.Fatal("accepted executable symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(dir)
	if err != nil {
		t.Fatal(err)
	}
	var output string
	_, err = runner.Run(context.Background(), ports.ExecSpec{Executable: ports.FFmpeg, Args: []string{"wrapper literal"}}, func(line ports.ProcessLine) { output = line.Text })
	if err != nil || output != "wrapper literal" {
		t.Fatalf("script: %q %v", output, err)
	}
}

func TestLinuxRunnerReapsRootAndStopsOrphanedChild(t *testing.T) {
	runner := linuxRunnerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	childPID := 0
	result, err := runner.Run(ctx, ports.ExecSpec{Executable: ports.YTDLP, Args: []string{"-test.run=^TestLinuxProcessHelper$"}, Env: []string{"DWNL_LINUX_HELPER=orphan"}}, func(line ports.ProcessLine) {
		if strings.HasPrefix(line.Text, "child=") {
			childPID, _ = strconv.Atoi(strings.TrimPrefix(line.Text, "child="))
		}
	})
	if err != nil || result.ExitCode != 0 || result.Canceled || childPID == 0 {
		t.Fatalf("orphan cleanup: %+v %v child=%d", result, err, childPID)
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	end := strings.LastIndexByte(string(data), ')')
	fields := strings.Fields(string(data[end+1:]))
	if fields[0] != "Z" && fields[0] != "X" {
		t.Fatalf("orphan alive: %s", data)
	}
}
