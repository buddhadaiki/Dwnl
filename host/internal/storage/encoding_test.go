package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDocumentsRejectMalformedUTF8(t *testing.T) {
	t.Run("ledger", func(t *testing.T) {
		s, err := Open(filepath.Join(t.TempDir(), "state"))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Put(terminalRecord(1)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Root(), "history.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		raw = bytes.Replace(raw, []byte("start-digest"), []byte{'b', 'a', 'd', 0xff}, 1)
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = Open(s.Root()); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("malformed ledger was accepted: %v", err)
		}
	})
	t.Run("preference", func(t *testing.T) {
		s, chosen, fallback := preferenceFixture(t)
		if err := s.SaveDestinationPreference(chosen); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Root(), "preferences.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		raw = bytes.Replace(raw, []byte("chosen"), []byte{'c', 'h', 0xff, 's', 'e', 'n'}, 1)
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		actual, err := s.PreferredDestination(fallback)
		if !errors.Is(err, ErrCorrupt) || actual != fallback {
			t.Fatalf("malformed preference: path=%q error=%v", actual, err)
		}
	})
}
