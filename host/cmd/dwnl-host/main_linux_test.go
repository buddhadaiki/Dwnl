package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxStartupRejectsOriginWithoutProtocolOutput(t *testing.T) {
	executable := os.Getenv("DWNL_TEST_HOST")
	if executable == "" {
		executable = filepath.Join(t.TempDir(), "dwnl-host")
		build := exec.Command("go", "build", "-o", executable, ".")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build host: %v %s", err, output)
		}
	}
	cmd := exec.Command(executable, "chrome-extension://private-invalid-origin/")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("accepted foreign origin")
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "stage=origin") || strings.Contains(stderr.String(), "private-invalid-origin") {
		t.Fatalf("unsafe startup output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
