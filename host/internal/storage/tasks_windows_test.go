package storage

import (
	"dwnl/host/internal/testutil"
	"encoding/binary"
	"errors"
	win "golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

// makeJunction creates a real Windows mount-point reparse object, without shell
// commands, administrator privileges, or a link outside the test's two roots.
func makeJunction(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	p, _ := win.UTF16PtrFromString(path)
	h, err := win.CreateFile(p, win.GENERIC_WRITE, 0, nil, win.OPEN_EXISTING, win.FILE_FLAG_BACKUP_SEMANTICS|win.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(h)
	substitute, _ := win.UTF16FromString(`\??\` + target)
	printName, _ := win.UTF16FromString(target)
	data := make([]byte, 16+len(substitute)*2+len(printName)*2)
	binary.LittleEndian.PutUint32(data, win.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(data[14:], uint16((len(printName)-1)*2))
	for i, value := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+i*2:], value)
	}
	var returned uint32
	retried := false
	err = testutil.RetryDirectoryNotEmpty(func() error {
		attemptErr := win.DeviceIoControl(h, win.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &returned, nil)
		if errors.Is(attemptErr, win.ERROR_DIR_NOT_EMPTY) && !retried {
			t.Log("retrying transient ERROR_DIR_NOT_EMPTY while creating the pinned junction fixture")
			retried = true
		}
		return attemptErr
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
}
func TestCleanupRejectsExternalJunction(t *testing.T) {
	base := t.TempDir()
	s, err := Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.PrepareTask("owned-job")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "user-owned")
	os.Mkdir(outside, 0700)
	marker := filepath.Join(outside, "preserve.txt")
	os.WriteFile(marker, []byte("user data"), 0600)
	makeJunction(t, filepath.Join(work, "external"), outside)
	if err = s.CleanupTask("owned-job"); err == nil {
		t.Fatal("cleanup accepted external junction")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "user data" {
		t.Fatalf("external data changed %q %v", got, err)
	}
}
func TestCleanupOwnedNestedFilesAndPreservesFinal(t *testing.T) {
	base := t.TempDir()
	s, err := Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.PrepareTask("owned-job")
	if err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(work, "media"), 0700)
	os.WriteFile(filepath.Join(work, "media", "part.bin"), []byte("temporary"), 0600)
	final := filepath.Join(base, "final.mp4")
	os.WriteFile(final, []byte("finished"), 0600)
	if err = s.CleanupTask("owned-job"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("work still exists: %v", err)
	}
	if got, err := os.ReadFile(final); err != nil || string(got) != "finished" {
		t.Fatalf("final changed: %q %v", got, err)
	}
	if err = s.CleanupTask(".."); err == nil {
		t.Fatal("traversal task ID accepted")
	}
}
func TestExactIntermediateCleanupPreservesReplacement(t *testing.T) {
	destination := testutil.TempDir(t)
	f, owned, err := CreateIntermediate(destination, "owned-job")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("partial")
	f.Close()
	if err = owned.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(owned.Path); !os.IsNotExist(err) {
		t.Fatalf("intermediate was not removed: %v", err)
	}
	f, owned, err = CreateIntermediate(destination, "replacement-job")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	preserved := owned.Path + ".original"
	if err = os.Rename(owned.Path, preserved); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(owned.Path, []byte("user replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = owned.Cleanup(); err == nil {
		t.Fatal("removed a replacement file")
	}
	if got, err := os.ReadFile(owned.Path); err != nil || string(got) != "user replacement" {
		t.Fatalf("replacement changed %q %v", got, err)
	}
}
