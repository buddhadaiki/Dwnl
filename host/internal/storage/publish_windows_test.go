package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"dwnl/host/internal/windows"
)

func createClosedPartial(t *testing.T, destination, jobID, data string) OwnedIntermediate {
	t.Helper()
	file, owned, err := CreateIntermediate(destination, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString(data); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Sync(); err != nil {
		file.Close()
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

func TestPublishExactIntermediateWithUnicodeDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "Downloads-ação-東京")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	owned := createClosedPartial(t, destination, "unicode-job", "completed media")
	final := filepath.Join(destination, "Filme café 😀.mp4")
	if err := owned.Publish(final); err != nil {
		t.Fatal(err)
	}
	requireContents(t, final, "completed media")
	if _, err := os.Stat(owned.Path); !os.IsNotExist(err) {
		t.Fatalf("source remains after publish: %v", err)
	}
	identity, err := windows.IdentifyFile(final)
	if err != nil || identity != owned.Identity {
		t.Fatalf("published another file: %+v, %v", identity, err)
	}
}
func TestPublishNeverOverwritesExistingTargetAndCanRetry(t *testing.T) {
	destination := t.TempDir()
	owned := createClosedPartial(t, destination, "collision-job", "owned media")
	final := filepath.Join(destination, "existing.mp4")
	if err := os.WriteFile(final, []byte("user media"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := owned.Publish(final); !errors.Is(err, os.ErrExist) {
		t.Fatalf("collision error = %v", err)
	}
	requireContents(t, final, "user media")
	requireContents(t, owned.Path, "owned media")
	retry := filepath.Join(destination, "existing (1).mp4")
	if err := owned.Publish(retry); err != nil {
		t.Fatal(err)
	}
	requireContents(t, retry, "owned media")
	requireContents(t, final, "user media")
}
func TestPublishRejectsReplacedIntermediateWithoutMovingIt(t *testing.T) {
	destination := t.TempDir()
	owned := createClosedPartial(t, destination, "replacement-job", "owned original")
	original := owned.Path + ".original"
	if err := os.Rename(owned.Path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(owned.Path, []byte("user replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(destination, "final.mp4")
	if err := owned.Publish(final); err == nil {
		t.Fatal("published the replacement at the old pathname")
	}
	requireContents(t, owned.Path, "user replacement")
	requireContents(t, original, "owned original")
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("replacement was moved before rejection: %v", err)
	}
}
func TestPublishRejectsDestinationOutsideApprovedParent(t *testing.T) {
	base := t.TempDir()
	approved := filepath.Join(base, "approved")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(approved, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	owned := createClosedPartial(t, approved, "outside-job", "owned media")
	final := filepath.Join(outside, "final.mp4")
	if err := owned.Publish(final); err == nil {
		t.Fatal("published outside approved parent")
	}
	requireContents(t, owned.Path, "owned media")
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("outside target changed: %v", err)
	}
}
func TestPublishRejectsReplacedParentEvenWhenSourceIdentityMatches(t *testing.T) {
	base := t.TempDir()
	destination := filepath.Join(base, "approved")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	owned := createClosedPartial(t, destination, "parent-job", "owned original")
	originalParent := destination + ".original"
	if err := os.Rename(destination, originalParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(originalParent, filepath.Base(owned.Path))
	// This alias has the same NTFS file ID. The independent directory identity
	// must reject publication into the replacement parent despite that match.
	if err := os.Link(original, owned.Path); err != nil {
		t.Fatal(err)
	}
	identity, err := windows.IdentifyFile(owned.Path)
	if err != nil || identity != owned.Identity {
		t.Fatalf("hardlink setup differs: %+v, %v", identity, err)
	}
	final := filepath.Join(destination, "final.mp4")
	if err := owned.Publish(final); err == nil {
		t.Fatal("published into a replacement destination")
	}
	requireContents(t, original, "owned original")
	requireContents(t, owned.Path, "owned original")
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("replacement parent target changed: %v", err)
	}
}
func TestPublishRejectsJunctionParentAndPreservesOutsideFiles(t *testing.T) {
	base := t.TempDir()
	destination := filepath.Join(base, "approved")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	owned := createClosedPartial(t, destination, "junction-job", "owned original")
	originalParent := destination + ".original"
	if err := os.Rename(destination, originalParent); err != nil {
		t.Fatal(err)
	}
	outsideReplacement := filepath.Join(outside, filepath.Base(owned.Path))
	if err := os.WriteFile(outsideReplacement, []byte("outside user media"), 0600); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, destination, outside)
	final := filepath.Join(destination, "final.mp4")
	if err := owned.Publish(final); err == nil {
		t.Fatal("publication followed a replacement junction")
	}
	requireContents(t, outsideReplacement, "outside user media")
	requireContents(t, filepath.Join(originalParent, filepath.Base(owned.Path)), "owned original")
	if _, err := os.Stat(filepath.Join(outside, "final.mp4")); !os.IsNotExist(err) {
		t.Fatalf("outside target changed: %v", err)
	}
}
