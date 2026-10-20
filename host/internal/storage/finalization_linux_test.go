package storage

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/windows"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxPreparedRecoveryRejectsChangedBytesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "media.mp4")
	body := []byte("verified synthetic bytes")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	fileID, err := windows.IdentifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := windows.IdentifyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	proof := contracts.PreparedFinalization{File: contracts.CompletedFile{Path: path, SizeBytes: uint64(len(body)), SHA256: hex.EncodeToString(hash[:])}, Identity: contracts.PhysicalIdentity(fileID), ParentIdentity: contracts.PhysicalIdentity(parentID)}
	if err = verifyPreparedFile(proof); err != nil {
		t.Fatal(err)
	}
	body[0] = 'X'
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if verifyPreparedFile(proof) == nil {
		t.Fatal("accepted changed bytes")
	}
	moved := path + ".old"
	if err = os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(moved, path); err != nil {
		t.Fatal(err)
	}
	if verifyPreparedFile(proof) == nil {
		t.Fatal("accepted symlink")
	}
}

func TestLinuxLocalPathsRemainCaseSensitive(t *testing.T) {
	for _, path := range []string{"/home/user/Downloads", "/home/user/Pasta com espaços"} {
		if !validLocalPath(path) {
			t.Fatalf("rejected local Linux path %q", path)
		}
	}
	for _, path := range []string{"relative/path", "/home/user/../other", "/home/user/\x00bad", "C:\\Users\\user"} {
		if validLocalPath(path) {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}
