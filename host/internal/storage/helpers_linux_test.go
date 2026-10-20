package storage

import (
	"os"
	"testing"
)

func createClosedPartial(t *testing.T, destination, jobID, data string) OwnedIntermediate {
	t.Helper()
	file, owned, err := CreateIntermediate(destination, jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.WriteString(data); err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return owned
}

func requireContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("file contents %q: %q, %v", path, data, err)
	}
}

// Shared storage cases use this historical name for a directory link. Linux
// exercises a symlink while the Windows implementation exercises a junction.
func makeJunction(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
