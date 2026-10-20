package windows

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ChooseFolder uses a fixed distro-provided desktop picker after user gesture.
func ChooseFolder(ctx context.Context) (string, bool, error) {
	if ctx == nil {
		return "", false, errors.New("folder picker context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/zenity", "--file-selection", "--directory", "--title=Choose the Dwnl download folder")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	output := &limitedPickerOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", true, nil
		}
		return "", false, errors.New("folder dialog unavailable; install zenity and run in a desktop session")
	}
	path := strings.TrimSuffix(string(output.data), "\n")
	if err := CheckPath(path, false); err != nil {
		return "", false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, errors.New("selected destination is not a directory")
	}
	return path, false, nil
}

type limitedPickerOutput struct{ data []byte }

func (b *limitedPickerOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 65536 {
		return 0, errors.New("folder picker output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
