package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"dwnl/host/internal/testutil"
)

// A failed real startup must identify its bounded stage/Win32 cause on stderr
// without publishing paths, origin data, or non-protocol bytes on stdout.
func TestFailedHostReportsSafeStage(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(testutil.TempDir(t), "diagnostic-host.exe")
	build := exec.Command(filepath.Join(repo, ".tools", "go", "bin", "go.exe"), "-C", filepath.Join(repo, "host"), "build", "-trimpath", "-ldflags", "-s -w", "-o", executable, "./cmd/dwnl-host")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual host: %v %s", err, output)
	}
	for _, tc := range []struct {
		stage   string
		args    []string
		pattern string
	}{
		{"origin", []string{"chrome-extension://sensitive-origin-value/"}, `^Dwnl host could not continue: stage=origin win32=0\r?\n$`},
		{"runner", []string{"chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/", "--parent-window=0"}, `^Dwnl host could not continue: stage=runner win32=[23]\r?\n$`},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			cmd := exec.Command(executable, tc.args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			cmd.Stdin = strings.NewReader("")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("startup failure exit = %v", err)
			}
			if stdout.Len() != 0 || !regexp.MustCompile(tc.pattern).MatchString(stderr.String()) {
				t.Fatalf("unsafe or missing startup diagnostic: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
