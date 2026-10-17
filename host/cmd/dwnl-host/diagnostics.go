package main

import (
	"errors"
	"fmt"
	"syscall"
)

type hostFailure struct {
	stage string
	cause error
}

func (e *hostFailure) Error() string { return "Dwnl host could not continue" }
func (e *hostFailure) Unwrap() error { return e.cause }

func failAt(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &hostFailure{stage: stage, cause: err}
}

// Never print err.Error(): filesystem/process errors may contain private paths,
// URLs or dependency output. Only a closed stage and numeric Win32 errno escape.
func failureDiagnostic(err error) string {
	stage := "unknown"
	var failure *hostFailure
	if errors.As(err, &failure) {
		switch failure.stage {
		case "origin", "runner", "paths", "engine", "store", "destination", "session", "transport":
			stage = failure.stage
		}
	}
	var code syscall.Errno
	errors.As(err, &code)
	return fmt.Sprintf("Dwnl host could not continue: stage=%s win32=%d", stage, uint64(code))
}
