package engine

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/storage"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The existing cancellation boundary is also a deterministic publication
// boundary: substitute a real file/directory as soon as its open handle permits
// rename, then let production publication run. No timer or production test hook.
type substituteIntermediateContext struct {
	context.Context
	destination     string
	directory       bool
	done            bool
	replacementPath string
	originalPath    string
	failure         error
}

func (c *substituteIntermediateContext) Err() error {
	if c.done {
		return c.Context.Err()
	}
	entries, err := os.ReadDir(c.destination)
	if err != nil {
		return c.Context.Err()
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".dwnl-") || !strings.HasSuffix(entry.Name(), ".partial") {
			continue
		}
		part := filepath.Join(c.destination, entry.Name())
		preserved := part + ".preserved"
		if c.directory {
			preserved = c.destination + "-preserved"
			if err := os.Rename(c.destination, preserved); err != nil {
				return c.Context.Err()
			}
			if err := os.Mkdir(c.destination, 0700); err != nil {
				c.failure = err
				c.done = true
				return err
			}
			c.originalPath = filepath.Join(preserved, entry.Name())
		} else {
			if err := os.Rename(part, preserved); err != nil {
				return c.Context.Err()
			}
			c.originalPath = preserved
		}
		c.replacementPath = part
		c.failure = os.WriteFile(part, []byte("user replacement"), 0600)
		c.done = true
		if c.failure != nil {
			return c.failure
		}
		break
	}
	return c.Context.Err()
}
func TestFinalizeRejectsSubstitutedIntermediateAndDestination(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "approved")
			if err := os.Mkdir(destination, 0700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "source")
			if err := os.WriteFile(source, []byte("owned media bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx := &substituteIntermediateContext{Context: context.Background(), destination: destination, directory: directory}
			_, err := finalize(ctx, source, destination, "Media", "mp4", "owned-job", contracts.CompletedFile{}, nil)
			if !ctx.done || ctx.failure != nil {
				t.Fatalf("substitution did not occur: done=%v error=%v", ctx.done, ctx.failure)
			}
			if err == nil {
				t.Fatal("substituted output was accepted")
			}
			data, readErr := os.ReadFile(ctx.replacementPath)
			if readErr != nil || string(data) != "user replacement" {
				t.Fatalf("replacement was moved or removed: data=%q error=%v", data, readErr)
			}
			if _, err := os.Stat(filepath.Join(destination, "Media.mp4")); !os.IsNotExist(err) {
				t.Fatalf("replacement was published: %v", err)
			}
			if _, err := os.Stat(ctx.originalPath); err != nil {
				t.Fatalf("original owned file was lost: %v", err)
			}
		})
	}
}
func publishedFixture(t *testing.T) (storage.OwnedIntermediate, os.FileInfo, int64, string, string) {
	t.Helper()
	destination := t.TempDir()
	f, owned, err := storage.CreateIntermediate(destination, "proof-job")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("original")
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "Media.mp4")
	if err = owned.Publish(target); err != nil {
		t.Fatal(err)
	}
	owned.Path = target
	sum := sha256.Sum256(data)
	unrelated := filepath.Join(destination, "unrelated.mp4")
	if err = os.WriteFile(unrelated, []byte("keep unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	return owned, info, int64(len(data)), hex.EncodeToString(sum[:]), unrelated
}
func TestFailedPublishedProofCleansOnlyOwnedFinal(t *testing.T) {
	for _, mutation := range []string{"size", "hash", "replacement"} {
		t.Run(mutation, func(t *testing.T) {
			owned, info, size, hash, unrelated := publishedFixture(t)
			switch mutation {
			case "size":
				f, err := os.OpenFile(owned.Path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write([]byte("corrupt"))
				if err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
			case "hash":
				if err := os.WriteFile(owned.Path, []byte("corrupt!"), 0600); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := os.Rename(owned.Path, owned.Path+".preserved"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(owned.Path, []byte("user replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := verifyPublished(context.Background(), owned, info, size, hash, "mp4")
			code(t, err, contracts.InvalidOutput)
			if mutation == "replacement" {
				data, err := os.ReadFile(owned.Path)
				if err != nil || string(data) != "user replacement" {
					t.Fatal("replacement was removed or modified")
				}
			} else {
				if _, err := os.Stat(owned.Path); !os.IsNotExist(err) {
					t.Fatalf("invalid owned final remains: %v", err)
				}
			}
			data, err := os.ReadFile(unrelated)
			if err != nil || string(data) != "keep unrelated" {
				t.Fatal("unrelated destination file changed")
			}
		})
	}
}
func TestVerifiedPublishedFileWinsLateCancellation(t *testing.T) {
	owned, info, size, hash, _ := publishedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := verifyPublished(ctx, owned, info, size, hash, "mp4")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != owned.Path || result.SHA256 != hash || result.SizeBytes != uint64(size) {
		t.Fatalf("verified final=%+v", result)
	}
	data, err := os.ReadFile(owned.Path)
	if err != nil || string(data) != "original" {
		t.Fatal("late cancellation removed a verified final")
	}
}
