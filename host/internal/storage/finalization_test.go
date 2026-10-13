package storage

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func savingRecord() Record {
	return Record{Fingerprint: "original-fingerprint", Snapshot: contracts.JobSnapshot{HostSessionID: "old-session", JobID: "journal-job", StartRequestID: "start-journal", Sequence: 7, State: contracts.StateSaving}}
}

func preparedFixture(t *testing.T) (*Store, Record, OwnedIntermediate, contracts.PreparedFinalization) {
	t.Helper()
	base := t.TempDir()
	s, err := Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(base, "approved")
	if err = os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	r := savingRecord()
	if err = s.Put(r); err != nil {
		t.Fatal(err)
	}
	owned := createClosedPartial(t, destination, "journal-job", "verified media bytes")
	sum := sha256.Sum256([]byte("verified media bytes"))
	proof := contracts.PreparedFinalization{
		File:           contracts.CompletedFile{Path: filepath.Join(destination, "final.mp4"), Format: "mp4", SizeBytes: 20, SHA256: hex.EncodeToString(sum[:]), VideoCodec: "h264", AudioCodec: "aac"},
		Identity:       contracts.PhysicalIdentity{VolumeSerialNumber: owned.Identity.VolumeSerialNumber, FileIndexHigh: owned.Identity.FileIndexHigh, FileIndexLow: owned.Identity.FileIndexLow},
		ParentIdentity: contracts.PhysicalIdentity{VolumeSerialNumber: owned.ParentIdentity.VolumeSerialNumber, FileIndexHigh: owned.ParentIdentity.FileIndexHigh, FileIndexLow: owned.ParentIdentity.FileIndexLow},
	}
	return s, r, owned, proof
}

// Dropping the preparation write, or trusting only the final pathname, breaks
// recovery when publication succeeded but the terminal snapshot was never saved.
func TestPreparedPublicationRecoversWithoutTerminalSave(t *testing.T) {
	s, original, owned, proof := preparedFixture(t)
	if err := s.PrepareFinalization("old-session", "journal-job", proof); err != nil {
		t.Fatal(err)
	}
	if err := owned.Publish(proof.File.Path); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	r, err := reopened.Find("old-session", "journal-job", "")
	if err != nil || r == nil {
		t.Fatalf("readback: %+v, %v", r, err)
	}
	if r.PreparedFinalization == nil {
		t.Fatal("durable prepared proof absent after reopening")
	}
	if r.TerminalConfirmed || r.Fingerprint != original.Fingerprint || !reflect.DeepEqual(r.Snapshot, original.Snapshot) {
		t.Fatalf("preparation changed task snapshot: %+v", r)
	}
	before, err := os.ReadFile(filepath.Join(s.Root(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, confirmed, err := reopened.PreparedCompletion(*r)
	if err != nil || !confirmed || got != proof.File {
		t.Fatalf("prepared completion: %+v, %v, %v", got, confirmed, err)
	}
	after, err := os.ReadFile(filepath.Join(s.Root(), "history.json"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("confirmation mutated private history: %v", err)
	}
}

// A progress or terminal writer usually has no proof in its in-memory snapshot.
func TestPutPreservesPreparedProofAndRetryUpdatesCandidate(t *testing.T) {
	s, r, _, proof := preparedFixture(t)
	if err := s.PrepareFinalization("old-session", "journal-job", proof); err != nil {
		t.Fatal(err)
	}
	proof.File.Path = filepath.Join(filepath.Dir(proof.File.Path), "final (1).mp4")
	if err := s.PrepareFinalization("old-session", "journal-job", proof); err != nil {
		t.Fatal(err)
	}
	r.Snapshot.Sequence++
	r.Snapshot.Progress = &contracts.Progress{Stage: contracts.StateSaving}
	if err := s.Put(r); err != nil {
		t.Fatal(err)
	}
	r.Snapshot.Sequence++
	r.Snapshot.State = contracts.StateCompleted
	r.Snapshot.Progress = nil
	r.Snapshot.CompletedFile = &proof.File
	r.TerminalConfirmed = true
	if err := s.Put(r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Find("old-session", "journal-job", "")
	if err != nil || got == nil || got.PreparedFinalization == nil || *got.PreparedFinalization != proof {
		t.Fatalf("Put dropped/replaced preparation: %+v, %v", got, err)
	}
}

func TestPreparedCompletionRejectsUnconfirmedCandidates(t *testing.T) {
	for _, scenario := range []string{"not published", "missing", "tampered", "same bytes replacement", "replaced parent", "junction parent"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, owned, proof := preparedFixture(t)
			if err := s.PrepareFinalization("old-session", "journal-job", proof); err != nil {
				t.Fatal(err)
			}
			if scenario != "not published" {
				if err := owned.Publish(proof.File.Path); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "missing":
				if err := os.Remove(proof.File.Path); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				if err := os.WriteFile(proof.File.Path, []byte("tampered media bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "same bytes replacement":
				if err := os.Rename(proof.File.Path, proof.File.Path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(proof.File.Path, []byte("verified media bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "replaced parent", "junction parent":
				parent := filepath.Dir(proof.File.Path)
				if err := os.Rename(parent, parent+".original"); err != nil {
					t.Fatal(err)
				}
				if scenario == "junction parent" {
					makeJunction(t, parent, parent+".original")
				} else {
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Link(filepath.Join(parent+".original", "final.mp4"), proof.File.Path); err != nil {
						t.Fatal(err)
					}
					id, err := hostwin.IdentifyFile(proof.File.Path)
					if err != nil || id != owned.Identity {
						t.Fatalf("hardlink physical fixture: %+v, %v", id, err)
					}
				}
			}
			r.PreparedFinalization = &proof
			if _, confirmed, err := s.PreparedCompletion(r); err != nil || confirmed {
				t.Fatalf("confirmed %s: %v, %v", scenario, confirmed, err)
			}
			if scenario == "same bytes replacement" {
				requireContents(t, proof.File.Path, "verified media bytes")
				requireContents(t, proof.File.Path+".original", "verified media bytes")
				// Exact fixture removal also proves recovery released its handles:
				// denied delete sharing would make either removal fail.
				for _, path := range []string{proof.File.Path, proof.File.Path + ".original"} {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestPrepareFinalizationRejectsUnknownJobAndInvalidProof(t *testing.T) {
	s, _, _, proof := preparedFixture(t)
	if err := s.PrepareFinalization("old-session", "unknown-job", proof); err == nil {
		t.Fatal("created preparation without an existing task")
	}
	invalid := proof
	invalid.File.AudioCodec = strings.Repeat("a", 513)
	if err := s.PrepareFinalization("old-session", "journal-job", invalid); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("invalid proof = %v", err)
	}
	invalid = proof
	invalid.Identity = contracts.PhysicalIdentity{}
	if err := s.PrepareFinalization("old-session", "journal-job", invalid); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("empty physical identity = %v", err)
	}
}

func TestPrivateRecordRejectsInvalidSnapshotAndProof(t *testing.T) {
	for _, scenario := range []string{"unsafe sequence", "invalid stage", "bytes exceed total", "nonfinite percent", "unknown error", "invalid metadata", "zero parent identity", "different completed proof"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, _, proof := preparedFixture(t)
			r.PreparedFinalization = &proof
			switch scenario {
			case "unsafe sequence":
				r.Snapshot.Sequence = contracts.MaxSafeInteger + 1
			case "invalid stage":
				r.Snapshot.Progress = &contracts.Progress{Stage: contracts.StateDownloading}
			case "bytes exceed total":
				completed, total := uint64(2), uint64(1)
				r.Snapshot.Progress = &contracts.Progress{Stage: contracts.StateSaving, CompletedBytes: &completed, TotalBytes: &total}
			case "nonfinite percent":
				percent := math.Inf(1)
				r.Snapshot.Progress = &contracts.Progress{Stage: contracts.StateSaving, Percent: &percent}
			case "unknown error":
				r.Snapshot.State = contracts.StateFailed
				r.Snapshot.Error = &contracts.ProtocolError{Code: "UNKNOWN_CODE", Message: "bad"}
			case "invalid metadata":
				proof.File.Format = "mp3"
				proof.File.VideoCodec = ""
				proof.File.AudioCodec = "aac"
				proof.File.MP3BitrateKbps = 128
			case "zero parent identity":
				proof.ParentIdentity = contracts.PhysicalIdentity{}
			case "different completed proof":
				completed := proof.File
				completed.SHA256 = strings.Repeat("0", 64)
				r.Snapshot.State = contracts.StateCompleted
				r.Snapshot.CompletedFile = &completed
			}
			if err := s.Put(r); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("accepted %s: %v", scenario, err)
			}
		})
	}
}

func TestPreparedLedgerStrictReadRejectsUnknownProofFields(t *testing.T) {
	s, r, _, proof := preparedFixture(t)
	r.PreparedFinalization = &proof
	if err := s.Put(r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Root(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	record := object["records"].([]any)[0].(map[string]any)
	record["preparedFinalization"].(map[string]any)["sourceUrl"] = "https://invalid.example/private"
	data, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.Root(), "history.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(s.Root()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unknown private proof field accepted: %v", err)
	}
}

func TestPreparedLedgerRejectsExplicitNullProof(t *testing.T) {
	s, r, _, proof := preparedFixture(t)
	r.PreparedFinalization = &proof
	if err := s.Put(r); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	object["records"].([]any)[0].(map[string]any)["preparedFinalization"] = nil
	data, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(s.Root()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("explicit null bypassed physical proof: %v", err)
	}
}
