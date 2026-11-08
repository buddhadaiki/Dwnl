package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtensionStatusNeedsTheInstalledFolder(t *testing.T) {
	install, state := t.TempDir(), t.TempDir()
	if status := extensionStatusFor(install, state, false); status != nil {
		t.Fatalf("a build without the extension folder offers it: %+v", status)
	}
	folder := filepath.Join(install, "extension")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if status := extensionStatusFor(install, state, false); status != nil {
		t.Fatal("a folder without a manifest is not an extension")
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	status := extensionStatusFor(install, state, false)
	if status == nil || status.Folder != folder || status.Connected || status.LastSeen != "" {
		t.Fatalf("installed, never connected: %+v", status)
	}
}

func TestExtensionContactKeepsTheLatestTime(t *testing.T) {
	install, state := t.TempDir(), t.TempDir()
	folder := filepath.Join(install, "extension")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 11, 5, 19, 58, 6, 0, time.UTC)
	if err := rememberExtensionConnected(state, first); err != nil {
		t.Fatal(err)
	}
	if err := rememberExtensionConnected(state, first.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if status := extensionStatusFor(install, state, false); status == nil || status.LastSeen != "2026-11-08T19:58:06Z" {
		t.Fatalf("latest contact not kept: %+v", status)
	}
	// A record from 0.3.0 and earlier holds the first contact in the same format.
	if err := os.WriteFile(filepath.Join(state, "extension-connected"), []byte("2026-11-05T19:58:06Z\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if status := extensionStatusFor(install, state, false); status.LastSeen != "2026-11-05T19:58:06Z" || status.Connected {
		t.Fatalf("0.3.0 record: %+v", status)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 1 {
		t.Fatalf("left %d files in the state", len(entries))
	}
}

// A connection recorded by an earlier run says nothing about this one: the
// extension may have been removed from Chrome since.
func TestOnlyAContactInThisRunCountsAsConnected(t *testing.T) {
	install, state := t.TempDir(), t.TempDir()
	folder := filepath.Join(install, "extension")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	earlier := &App{executable: filepath.Join(install, "Dwnl.exe"), stateRoot: state}
	earlier.extensionConnected()
	if status := earlier.extensionStatus(); status == nil || !status.Connected || status.LastSeen == "" {
		t.Fatalf("contact in this run: %+v", status)
	}
	seen := earlier.extensionStatus().LastSeen

	later := &App{executable: filepath.Join(install, "Dwnl.exe"), stateRoot: state}
	status := later.extensionStatus()
	if status == nil || status.Connected {
		t.Fatalf("a later run without contact reports the extension connected: %+v", status)
	}
	if status.LastSeen != seen {
		t.Fatalf("the earlier contact was not kept: %q, want %q", status.LastSeen, seen)
	}
	later.extensionConnected()
	if status := later.extensionStatus(); !status.Connected || status.LastSeen < seen {
		t.Fatalf("contact in the later run: %+v", status)
	}
}

func TestOnlyTheBrowserBridgeCountsAsTheExtension(t *testing.T) {
	if fromExtension(secondInstanceID) {
		t.Fatal("a second app instance is not the extension")
	}
	if fromExtension(developmentCheckID) {
		t.Fatal("the development check is not the extension")
	}
	if !fromExtension("0b6d3c1e-2f4a-4c5b-9d7e-8f9a0b1c2d3e") {
		t.Fatal("panel requests come from the extension")
	}
}
