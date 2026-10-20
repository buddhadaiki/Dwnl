package windows

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxRejectsSymlinkComponents(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(link, "missing")} {
		if err := CheckPath(path, true); err == nil {
			t.Fatalf("accepted symlink %s", path)
		}
		if err := CreateDirectories(path, true); err == nil {
			t.Fatalf("created through symlink %s", path)
		}
	}
	if err := CheckPath(filepath.Join(real, "missing"), true); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxPublicationCollisionAndIdentity(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "part"), filepath.Join(root, "final")
	if err := os.WriteFile(source, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	parent, err := IdentifyFile(root)
	if err != nil {
		t.Fatal(err)
	}
	id, err := IdentifyFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = PublishExactFile(source, target, id, parent); !errors.Is(err, os.ErrExist) {
		t.Fatalf("collision: %v", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "existing" {
		t.Fatal("existing target changed")
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err = PublishExactFile(source, target, id, FileIdentity{}); err == nil {
		t.Fatal("accepted replaced parent")
	}
	if err = os.Rename(source, source+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = PublishExactFile(source, target, id, parent); err == nil {
		t.Fatal("published replacement")
	}
	if err = RemoveExactFile(source, id); err == nil {
		t.Fatal("removed replacement")
	}
	if err = PublishExactFile(source+"-old", target, id, parent); err != nil {
		t.Fatal(err)
	}
	got, err := IdentifyFile(target)
	if err != nil || got != id {
		t.Fatalf("published identity %v, %v", got, err)
	}
	if _, err = os.Stat(source + "-old"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source not cleaned: %v", err)
	}
}

func TestLinuxOwnedTreeRejectsLinksBeforeDeletion(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "owned")
	if err := os.Mkdir(tree, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "keep"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(tree, "link")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOwnedTree(tree, func() error { return nil }); err == nil {
		t.Fatal("accepted symlink tree")
	}
	if _, err := os.Stat(filepath.Join(tree, "keep")); err != nil {
		t.Fatal("deleted before validating tree")
	}
	if err := os.Remove(filepath.Join(tree, "link")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOwnedTree(tree, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxLeaseExclusiveAndRelease(t *testing.T) {
	scope := t.TempDir()
	first, busy, err := AcquireLease(scope, false)
	if err != nil || busy {
		t.Fatalf("first %v %v", busy, err)
	}
	defer first.Close()
	second, busy, err := AcquireLease(scope, false)
	if err != nil || !busy || second != nil {
		t.Fatalf("second %v %v %v", second, busy, err)
	}
	acquired := make(chan *Lease, 1)
	errs := make(chan error, 1)
	go func() {
		lease, _, err := AcquireLease(scope, true)
		if err != nil {
			errs <- err
			return
		}
		acquired <- lease
	}()
	select {
	case lease := <-acquired:
		lease.Close()
		t.Fatal("blocking lease acquired too soon")
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(40 * time.Millisecond):
	}
	first.Close()
	select {
	case lease := <-acquired:
		lease.Close()
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("lease did not release")
	}
}

func TestLinuxXDGDirectoriesAreParsedNotExecuted(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "config")
	if err := os.Mkdir(config, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	file := filepath.Join(config, "user-dirs.dirs")
	if err := os.WriteFile(file, []byte("XDG_DOWNLOAD_DIR=\"$HOME/Baixados\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, destination, err := DefaultPaths()
	if err != nil || state != filepath.Join(home, "data", "Dwnl", "state") || destination != filepath.Join(home, "Baixados", "Dwnl") {
		t.Fatalf("paths %q %q %v", state, destination, err)
	}
	if err := os.WriteFile(file, []byte("XDG_DOWNLOAD_DIR=\"$(touch /tmp/should-never-run)\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = DefaultPaths(); err == nil {
		t.Fatal("accepted command expression")
	}
}
