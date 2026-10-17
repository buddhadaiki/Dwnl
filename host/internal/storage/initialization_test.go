package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenKeepsAbsentOrEmptyStateReadOnly(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "absent"
		if existing {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			if existing {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Open(root); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(root)
			if existing && (err != nil || len(entries) != 0) {
				t.Fatalf("Open wrote empty state: entries=%v err=%v", entries, err)
			}
			if !existing && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Open created absent state: entries=%v err=%v", entries, err)
			}
		})
	}
}

// Validating at Open is insufficient: unknown content may appear before the
// explicit initialization acquires the shared ledger lease.
func TestInitializeOwnershipRevalidatesBeforeClaiming(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "unknown.txt")
	const content = "do not claim or overwrite"
	if err = os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.InitializeOwnership(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("claimed state changed since Open: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "unknown.txt" {
		t.Fatalf("modified rejected state: entries=%v err=%v", entries, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("modified unknown content: %q, %v", got, err)
	}
}
