package testutil

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestTransientDirectoryNotEmptyIsRetried(t *testing.T) {
	calls := 0
	err := removeTempDir("owned", func(path string) error {
		calls++
		if calls == 1 {
			return &os.PathError{Op: "unlinkat", Path: path, Err: syscall.ERROR_DIR_NOT_EMPTY}
		}
		return nil
	}, time.Now, func(time.Duration) {})
	if err != nil || calls != 2 {
		t.Fatalf("transient directory removal: calls=%d error=%v", calls, err)
	}
}
func TestPersistentDirectoryNotEmptyFailsWithinDeadline(t *testing.T) {
	clock := time.Unix(0, 0)
	calls := 0
	err := removeTempDir("owned", func(string) error { calls++; return syscall.ERROR_DIR_NOT_EMPTY }, func() time.Time { return clock }, func(delay time.Duration) { clock = clock.Add(delay) })
	if !errors.Is(err, syscall.ERROR_DIR_NOT_EMPTY) || clock.Sub(time.Unix(0, 0)) != 2*time.Second || calls > 201 {
		t.Fatalf("unbounded or hidden failure: calls=%d elapsed=%s error=%v", calls, clock.Sub(time.Unix(0, 0)), err)
	}
}

func TestDifferentErrorAfterTransientCleanupIsReturned(t *testing.T) {
	calls := 0
	err := removeTempDir("owned", func(string) error {
		calls++
		if calls == 1 {
			return syscall.ERROR_DIR_NOT_EMPTY
		}
		return syscall.ERROR_ACCESS_DENIED
	}, time.Now, func(time.Duration) {})
	if err != syscall.ERROR_ACCESS_DENIED || calls != 2 {
		t.Fatalf("changed failure suppressed: calls=%d error=%v", calls, err)
	}
}
func TestOtherCleanupErrorsAreNotRetried(t *testing.T) {
	for _, want := range []error{syscall.ERROR_ACCESS_DENIED, syscall.Errno(32), errors.New("unexpected filesystem failure")} {
		calls := 0
		err := removeTempDir("owned", func(string) error { calls++; return want }, time.Now, func(time.Duration) { t.Fatal("unexpected retry sleep") })
		if err != want || calls != 1 {
			t.Fatalf("changed error: calls=%d error=%v", calls, err)
		}
	}
}
