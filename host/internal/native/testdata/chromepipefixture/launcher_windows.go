package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	win "golang.org/x/sys/windows"
)

// This launcher is already in the real runner's owned Job Object. All of its
// children inherit that job; it never changes job membership or kills by PID.
func launchFixture() error {
	fixture, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(fixture, os.Args[1:]...)
	cmd.Stderr = os.Stderr // *os.File: no os/exec copying goroutine can hold Wait.
	input, output := os.Getenv("DWNL_PIPE_FIXTURE_INPUT"), os.Getenv("DWNL_PIPE_FIXTURE_OUTPUT")
	if os.Getenv("DWNL_PIPE_FIXTURE_LAUNCHER") == "cmd" {
		cmd = exec.Command(os.Getenv("SYSTEMROOT") + `\System32\cmd.exe`)
		cmd.Stderr = os.Stderr
		// Match Chromium's literal CMD grammar, including native pipe redirection.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`"%s" /d /s /c ""%s" chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/ --parent-window=0" < %s > %s`, cmd.Path, fixture, input, output)}
	} else {
		open := func(name string, access uint32) (*os.File, error) {
			path, err := win.UTF16PtrFromString(name)
			if err != nil {
				return nil, err
			}
			sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), InheritHandle: 1}
			handle, err := win.CreateFile(path, access|win.SYNCHRONIZE, 0, &sa, win.OPEN_EXISTING, win.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				return nil, err
			}
			return os.NewFile(uintptr(handle), "chrome-client"), nil
		}
		in, err := open(input, win.FILE_READ_DATA)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := open(output, win.FILE_WRITE_DATA)
		if err != nil {
			return err
		}
		defer out.Close()
		cmd.Stdin, cmd.Stdout = in, out
	}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "DWNL_PIPE_FIXTURE_MODE=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "DWNL_PIPE_FIXTURE_MODE="+os.Getenv("DWNL_PIPE_FIXTURE_CHILD_MODE"))
	return cmd.Run()
}
