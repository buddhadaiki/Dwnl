package storage

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func terminalRecord(i int) Record {
	return Record{Fingerprint: "start-digest", TerminalConfirmed: true, Snapshot: contracts.JobSnapshot{HostSessionID: "old-session", JobID: "job-" + strconv.Itoa(i), StartRequestID: "start-" + strconv.Itoa(i), Sequence: 2, State: contracts.StateCanceled}}
}
func TestCorruptLedgerDoesNotClaimSuccess(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put(terminalRecord(1)); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "history.json"), []byte(`{"version":1,"records":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Find("old-session", "job-1", ""); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt read = %v", err)
	}
	if err = s.Put(terminalRecord(2)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("overwrote corrupt ledger: %v", err)
	}
}
func TestHistoryCapsAtTwentyAndClearKeepsDownloads(t *testing.T) {
	base := t.TempDir()
	s, err := Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	download := filepath.Join(base, "user.mp4")
	os.WriteFile(download, []byte("keep me"), 0600)
	for i := 0; i < 23; i++ {
		r := terminalRecord(i)
		if err = s.Put(r); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.History()
	if err != nil || len(records) != 20 {
		t.Fatalf("history size = %d, %v", len(records), err)
	}
	if records[0].Snapshot.JobID != "job-3" || records[19].Snapshot.JobID != "job-22" {
		t.Fatalf("wrong eviction: %v", records)
	}
	if err = s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(download); err != nil || string(got) != "keep me" {
		t.Fatalf("download changed: %q, %v", got, err)
	}
}
func TestVerifyCompletedRejectsTamperedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "final.mp4")
	os.WriteFile(file, []byte("original"), 0600)
	sum := sha256.Sum256([]byte("original"))
	c := contracts.CompletedFile{Path: file, Format: "mp4", SizeBytes: 8, SHA256: hex.EncodeToString(sum[:])}
	if err := VerifyCompleted(c); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, []byte("tampered"), 0600)
	if err := VerifyCompleted(c); err == nil {
		t.Fatal("claimed modified output is valid")
	}
}
func TestLedgerDoesNotPersistSourceOrRawError(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	r := terminalRecord(1)
	r.Snapshot.State = contracts.StateFailed
	r.Snapshot.Error = &contracts.ProtocolError{Code: contracts.EngineError, Message: "https://cdn.example.invalid/private-media?token=do-not-store"}
	if err = s.Put(r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(s.Root(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(b, &object); err != nil {
		t.Fatal(err)
	}
	if _, found := object["sourceUrl"]; found {
		t.Fatal("source leaked")
	}
	if strings.Contains(string(b), "private-media") || strings.Contains(string(b), "do-not-store") {
		t.Fatal("raw engine error leaked into the private ledger")
	}
}
