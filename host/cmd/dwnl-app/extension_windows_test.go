package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtensionStatusNeedsTheInstalledFolder(t *testing.T) {
	install, state := t.TempDir(), t.TempDir()
	if status := extensionStatusFor(install, state); status != nil {
		t.Fatalf("a build without the extension folder offers it: %+v", status)
	}
	folder := filepath.Join(install, "extension")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if status := extensionStatusFor(install, state); status != nil {
		t.Fatal("a folder without a manifest is not an extension")
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	status := extensionStatusFor(install, state)
	if status == nil || status.Folder != folder || status.Connected {
		t.Fatalf("installed, never connected: %+v", status)
	}
}

func TestExtensionConnectionIsRemembered(t *testing.T) {
	install, state := t.TempDir(), t.TempDir()
	folder := filepath.Join(install, "extension")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := rememberExtensionConnected(state); err != nil {
		t.Fatal(err)
	}
	// A second connection, as every panel request makes, keeps the first record.
	if err := rememberExtensionConnected(state); err != nil {
		t.Fatal(err)
	}
	if status := extensionStatusFor(install, state); status == nil || !status.Connected {
		t.Fatalf("connection not remembered: %+v", status)
	}
}

func TestOnlyTheBrowserBridgeCountsAsTheExtension(t *testing.T) {
	if fromExtension(secondInstanceID) {
		t.Fatal("a second app instance is not the extension")
	}
	if !fromExtension("0b6d3c1e-2f4a-4c5b-9d7e-8f9a0b1c2d3e") {
		t.Fatal("panel requests come from the extension")
	}
}
