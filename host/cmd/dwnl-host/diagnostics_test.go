package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestDiagnosticRetainsWin32CauseWithoutPrivateText(t *testing.T) {
	private := fmt.Errorf("secret URL https://private.invalid/token: %w", &os.PathError{Op: "open", Path: `C:\private\source`, Err: syscall.Errno(5)})
	failure := failAt("store", private)
	if got := failureDiagnostic(failure); got != "Dwnl host could not continue: stage=store win32=5" {
		t.Fatalf("private or incorrect diagnostic: %q", got)
	}
	var code syscall.Errno
	if !errors.As(failure, &code) || code != 5 {
		t.Fatal("stage wrapper lost the original Win32 cause")
	}
}

func TestDiagnosticRejectsUntrustedStage(t *testing.T) {
	failure := failAt("private-path-or-URL", errors.New("secret dependency log"))
	if got := failureDiagnostic(failure); got != "Dwnl host could not continue: stage=unknown win32=0" {
		t.Fatalf("unbounded diagnostic: %q", got)
	}
}

func TestStageWrapperPreservesSuccessfulEOF(t *testing.T) {
	if err := failAt("transport", nil); err != nil {
		t.Fatalf("successful connection became a failure: %v", err)
	}
}
