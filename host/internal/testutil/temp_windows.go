// Package testutil supplies test fixtures only; production packages never import it.
package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TempDir(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.Abs(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatal("fixture directory is outside the temp root")
	}
	t.Cleanup(func() {
		retried := false
		pause := func(delay time.Duration) {
			if !retried {
				t.Log("retrying transient ERROR_DIR_NOT_EMPTY in owned fixture cleanup")
				retried = true
			}
			time.Sleep(delay)
		}
		if err := removeTempDir(dir, os.RemoveAll, time.Now, pause); err != nil {
			t.Errorf("owned fixture cleanup: %v", err)
		}
	})
	return dir
}

func removeTempDir(path string, remove func(string) error, now func() time.Time, sleep func(time.Duration)) error {
	return retryDirectoryNotEmpty(func() error { return remove(path) }, now, sleep)
}

// RetryDirectoryNotEmpty is only for controlled fixture setup, on the same
// pinned object. It never retries access/sharing errors or changes permissions.
func RetryDirectoryNotEmpty(operation func() error) error {
	return retryDirectoryNotEmpty(operation, time.Now, time.Sleep)
}

func retryDirectoryNotEmpty(operation func() error, now func() time.Time, sleep func(time.Duration)) error {
	// Only the observed transient error is retryable; all other errors return
	// immediately. A persistent failure remains visible at the finite deadline.
	deadline := now().Add(2 * time.Second)
	for {
		err := operation()
		if err == nil || !errors.Is(err, syscall.ERROR_DIR_NOT_EMPTY) || !now().Before(deadline) {
			return err
		}
		sleep(10 * time.Millisecond)
	}
}
