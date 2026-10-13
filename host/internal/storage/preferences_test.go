package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func preferenceFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	base := t.TempDir()
	s, err := Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	chosen, fallback := filepath.Join(base, "chosen"), filepath.Join(base, "default")
	for _, path := range []string{chosen, fallback} {
		if err = os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return s, chosen, fallback
}

// Removing the actual preference write breaks the next host session's default.
func TestDestinationPreferencePersistsAcrossStoreReopen(t *testing.T) {
	s, chosen, fallback := preferenceFixture(t)
	if err := s.SaveDestinationPreference(chosen); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.PreferredDestination(fallback)
	if err != nil || got != chosen {
		t.Fatalf("preferred destination = %q, %v", got, err)
	}
	if err = reopened.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	got, err = reopened.PreferredDestination(fallback)
	if err != nil || got != chosen {
		t.Fatalf("history clear removed destination = %q, %v", got, err)
	}
}

func TestDestinationPreferenceSafelyFallsBackWhenFolderUnavailable(t *testing.T) {
	for _, scenario := range []string{"missing", "file", "junction"} {
		t.Run(scenario, func(t *testing.T) {
			s, chosen, fallback := preferenceFixture(t)
			if err := s.SaveDestinationPreference(chosen); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(chosen); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "file":
				if err := os.WriteFile(chosen, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "junction":
				makeJunction(t, chosen, fallback)
			}
			got, err := s.PreferredDestination(fallback)
			if err != nil || got != fallback {
				t.Fatalf("unavailable %s = %q, %v", scenario, got, err)
			}
		})
	}
}

func TestDestinationPreferenceRejectsUnsafePathsAndCorruptConfig(t *testing.T) {
	s, chosen, fallback := preferenceFixture(t)
	for _, path := range []string{"relative", `\\server\share`, filepath.Join(chosen, "missing")} {
		if err := s.SaveDestinationPreference(path); err == nil {
			t.Fatalf("accepted unsafe destination %q", path)
		}
	}
	if err := s.SaveDestinationPreference(chosen); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "preferences.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"owner", "unknown field", "UNC"} {
		t.Run(scenario, func(t *testing.T) {
			copy := map[string]any{}
			for k, v := range object {
				copy[k] = v
			}
			switch scenario {
			case "owner":
				copy["owner"] = "other-user"
			case "unknown field":
				copy["url"] = "private"
			case "UNC":
				copy["destinationPath"] = `\\server\share`
			}
			encoded, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := s.PreferredDestination(fallback)
			if !errors.Is(err, ErrCorrupt) || got != fallback {
				t.Fatalf("corrupt %s = %q, %v", scenario, got, err)
			}
			if err = s.SaveDestinationPreference(chosen); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("overwrote corrupt %s: %v", scenario, err)
			}
		})
	}
}
