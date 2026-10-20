package main

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletedFileVerification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	data := []byte("same-sized-original-media")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	file := contracts.CompletedFile{Path: path, SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	if err := verifyCompletedContext(context.Background(), file, true); err != nil {
		t.Fatal(err)
	}
	changed := append([]byte{}, data...)
	changed[0] = 'X'
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCompletedContext(context.Background(), file, true); err == nil {
		t.Fatal("modified file accepted")
	}
	if err := verifyCompletedContext(context.Background(), file, false); err != nil {
		t.Fatalf("opening parent should not hash media: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyCompletedContext(ctx, file, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	file.SizeBytes++
	if err := verifyCompletedContext(context.Background(), file, false); err == nil {
		t.Fatal("changed size accepted")
	}
}
