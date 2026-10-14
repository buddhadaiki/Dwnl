package engine

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/testutil"
	"dwnl/host/internal/windows"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func directMedia(t *testing.T) (string, string, []byte) {
	t.Helper()
	destination := testutil.TempDir(t)
	source := filepath.Join(testutil.TempDir(t), "processed")
	data := []byte("verified host media bytes")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	return source, destination, data
}
func dtoIdentity(id windows.FileIdentity) contracts.PhysicalIdentity {
	return contracts.PhysicalIdentity{VolumeSerialNumber: id.VolumeSerialNumber, FileIndexHigh: id.FileIndexHigh, FileIndexLow: id.FileIndexLow}
}
func TestPreparationRecordsProofBeforeFinalExists(t *testing.T) {
	source, destination, data := directMedia(t)
	sum := sha256.Sum256(data)
	var captured contracts.PreparedFinalization
	called := false
	prepare := func(p contracts.PreparedFinalization) error {
		called = true
		captured = p
		if _, err := os.Stat(p.File.Path); !os.IsNotExist(err) {
			t.Fatalf("final was visible before preparation: %v", err)
		}
		parts, err := filepath.Glob(filepath.Join(destination, "*.partial"))
		if err != nil || len(parts) != 1 {
			t.Fatalf("exclusive intermediate=%v error=%v", parts, err)
		}
		fileID, err := windows.IdentifyFile(parts[0])
		if err != nil {
			t.Fatal(err)
		}
		parentID, err := windows.IdentifyFile(destination)
		if err != nil {
			t.Fatal(err)
		}
		if p.Identity != dtoIdentity(fileID) || p.ParentIdentity != dtoIdentity(parentID) {
			t.Fatalf("prepared identities=%+v", p)
		}
		return nil
	}
	result, err := finalize(context.Background(), source, destination, "Media", "mp4", "prepared-job", contracts.CompletedFile{VideoCodec: "h264", AudioCodec: "aac"}, prepare)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("publication completed without preparing journal proof")
	}
	want := contracts.CompletedFile{Path: filepath.Join(destination, "Media.mp4"), Format: "mp4", SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:]), VideoCodec: "h264", AudioCodec: "aac"}
	if !reflect.DeepEqual(captured.File, want) || !reflect.DeepEqual(result, want) {
		t.Fatalf("prepared=%+v final=%+v want=%+v", captured.File, result, want)
	}
}
func TestPreparationFailurePreventsPublicationAndCleansPart(t *testing.T) {
	source, destination, _ := directMedia(t)
	called := false
	_, err := finalize(context.Background(), source, destination, "Media", "mp4", "prepared-job", contracts.CompletedFile{}, func(contracts.PreparedFinalization) error {
		called = true
		return errors.New("journal unavailable: private details")
	})
	if !called {
		t.Fatal("journal callback was skipped")
	}
	code(t, err, contracts.DestinationError)
	files, _ := os.ReadDir(destination)
	if len(files) != 0 {
		t.Fatalf("preparation failure published or leaked files: %v", files)
	}
}

func TestPreparationCancellationPreventsPublicationAndCleansPart(t *testing.T) {
	source, destination, _ := directMedia(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	_, err := finalize(ctx, source, destination, "Media", "mp4", "prepared-job", contracts.CompletedFile{}, func(contracts.PreparedFinalization) error {
		called = true
		cancel()
		return nil
	})
	if !called || !errors.Is(err, context.Canceled) {
		t.Fatalf("preparation cancellation: called=%v err=%v", called, err)
	}
	files, err := os.ReadDir(destination)
	if err != nil || len(files) != 0 {
		t.Fatalf("canceled preparation published or leaked files: %v, %v", files, err)
	}
}
func TestCollisionRetriesPrepareEachActualCandidate(t *testing.T) {
	source, destination, _ := directMedia(t)
	existing := filepath.Join(destination, "Media.mp4")
	if err := os.WriteFile(existing, []byte("user existing"), 0600); err != nil {
		t.Fatal(err)
	}
	var prepared []contracts.PreparedFinalization
	result, err := finalize(context.Background(), source, destination, "Media", "mp4", "prepared-job", contracts.CompletedFile{}, func(p contracts.PreparedFinalization) error {
		prepared = append(prepared, p)
		if filepath.Base(p.File.Path) == "Media (1).mp4" {
			if _, err := os.Stat(p.File.Path); !os.IsNotExist(err) {
				t.Fatal("retry final appeared before preparation")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 2 || prepared[0].File.Path != existing || prepared[1].File.Path != filepath.Join(destination, "Media (1).mp4") || prepared[1].File.Path != result.Path {
		t.Fatalf("prepared collision paths=%+v result=%+v", prepared, result)
	}
	if prepared[0].Identity != prepared[1].Identity || prepared[0].ParentIdentity != prepared[1].ParentIdentity {
		t.Fatal("retry lost captured physical identity")
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "user existing" {
		t.Fatal("collision overwrote user file")
	}
}
func TestExecutePreparesValidatedMp3Metadata(t *testing.T) {
	e, _, a := fixtureEngine(t)
	plan := localPlan(t, a, contracts.ModeMP3)
	var captured contracts.PreparedFinalization
	called := false
	plan.PrepareFinalization = func(p contracts.PreparedFinalization) error {
		called = true
		captured = p
		if _, err := os.Stat(p.File.Path); !os.IsNotExist(err) {
			t.Fatal("MP3 was published before journal preparation")
		}
		return nil
	}
	result, err := e.Execute(context.Background(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("Execute did not pass its journal callback")
	}
	if !reflect.DeepEqual(captured.File, result) || captured.File.AudioCodec != "mp3" || captured.File.VideoCodec != "" || captured.File.MP3BitrateKbps != 192 {
		t.Fatalf("prepared=%+v final=%+v", captured.File, result)
	}
}
