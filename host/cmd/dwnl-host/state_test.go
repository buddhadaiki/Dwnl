package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	"dwnl/host/internal/storage"
	"dwnl/host/internal/testutil"
	"dwnl/host/internal/windows"
)

// A first diagnostic handshake must leave state that the next host can open,
// even when no job or destination preference has ever been saved.
func TestHostStoreReopensAfterRealDependencyProbe(t *testing.T) {
	bin := os.Getenv("DWNL_MEDIA_BIN")
	if bin == "" {
		bin = filepath.Join("..", "..", "..", ".tools", "media", "bin")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"yt-dlp", "deno", "ffmpeg", "ffprobe"} {
		if _, err = os.Stat(filepath.Join(bin, name+".exe")); err != nil {
			t.Skip("private media tools unavailable: " + err.Error())
		}
	}
	root := filepath.Join(testutil.TempDir(t), "state")
	if _, err = openHostStore(root); err != nil {
		t.Fatal(err)
	}
	runner, err := windows.NewRunner(bin)
	if err != nil {
		t.Fatal(err)
	}
	media, err := engine.New(engine.Config{HostSessionID: "first-host", Runner: runner, PrivateBinDir: bin, AnalysisRoot: filepath.Join(root, "analyses")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = media.Probe(ctx); err != nil {
		t.Fatalf("real offline dependency probe: %v", err)
	}
	if media.Capabilities() != (contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}) {
		t.Fatalf("probe did not exercise the private adapter: %+v", media.Capabilities())
	}
	entries, err := os.ReadDir(filepath.Join(root, "analyses"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("probe cache was not created and cleaned: entries=%v err=%v", entries, err)
	}
	reopened, err := openHostStore(root)
	if err != nil {
		t.Fatalf("second host startup rejected state left by its first probe: %v", err)
	}
	if history, err := reopened.History(); err != nil || len(history) != 0 {
		t.Fatalf("diagnostic-only startup changed history: history=%v err=%v", history, err)
	}
	if _, err = os.Stat(filepath.Join(root, "history.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diagnostic-only startup wrote history: %v", err)
	}
}

func TestHostStoreRejectsUnownedOrInvalidState(t *testing.T) {
	for _, scenario := range []string{"unowned file", "unowned analyses directory", "malformed owner", "foreign owner"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "state")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			name, content := "unowned.txt", []byte("preserve unknown content")
			switch scenario {
			case "unowned analyses directory":
				name, content = "analyses", nil
				if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
					t.Fatal(err)
				}
			case "malformed owner":
				name, content = "owner.json", []byte(`{"version":`)
			case "foreign owner":
				name, content = "owner.json", []byte(`{"version":1,"owner":"other-user"}`)
			}
			if content != nil {
				if err := os.WriteFile(filepath.Join(root, name), content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := openHostStore(root); !errors.Is(err, storage.ErrCorrupt) {
				t.Fatalf("claimed %s: %v", scenario, err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != name {
				t.Fatalf("modified rejected state: entries=%v err=%v", entries, err)
			}
			if content != nil {
				got, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(got) != string(content) {
					t.Fatalf("modified rejected content: %q, %v", got, err)
				}
			}
		})
	}
}
