package main

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxRuntimeLeaseExcludesInstaller(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	release, err := acquireRuntimeUse(filepath.Join(runtime, "dwnl-host"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fd, err := unix.Open(filepath.Join(root, ".dwnl-use.lock"), unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		t.Fatal("installer obtained exclusive lock while host was active")
	}
	second, err := acquireRuntimeUse(filepath.Join(runtime, "dwnl-host"))
	if err != nil {
		t.Fatal("second host cannot share lifetime lock", err)
	}
	second()
}
