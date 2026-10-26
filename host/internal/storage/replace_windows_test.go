package storage

import (
	"dwnl/host/internal/testutil"
	hostwin "dwnl/host/internal/windows"
	"path/filepath"
	"testing"
	"time"
)

// Antivirus and indexers briefly hold freshly written files open without delete
// sharing; the ledger replace must outlast that instead of failing the task.
func TestLedgerWriteSurvivesBriefWindowsSharingDenial(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "state")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put(terminalRecord(1)); err != nil {
		t.Fatal(err)
	}
	unlock, err := hostwin.LockPath(filepath.Join(root, "history.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() { time.Sleep(40 * time.Millisecond); unlock(); close(released) }()
	err = s.Put(terminalRecord(2))
	<-released
	if err != nil {
		t.Fatal(err)
	}
	records, err := s.History()
	if err != nil || len(records) != 2 {
		t.Fatalf("history after transient denial = %d, %v", len(records), err)
	}
}
