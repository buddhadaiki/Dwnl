package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/storage"
)

func TestRecoveryOwnsAllRetryAttemptsAndRepairsPersistedLegacy(t *testing.T) {
	firstEngine := newFake()
	c := fixtureConfig(t, firstEngine)
	s := startService(t, c)
	request := selection(t, s, "retried-request")
	task := enqueue(t, s, request)
	waitStarted(t, firstEngine)
	s.Close()

	retryEngine := newFake()
	c.Engine = retryEngine
	retry := startService(t, c)
	command(t, retry, "continue", `{}`)
	waitStarted(t, retryEngine)
	retryEngine.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, retry, task.ID) == "completed" })
	records, err := retry.store.History()
	if err != nil || len(records) != 2 {
		t.Fatalf("retry attempts: %d %v", len(records), err)
	}
	// Simulate the persisted projection produced by the prior buggy version.
	retry.mu.Lock()
	retry.legacy = []contracts.JobSnapshot{records[0].Snapshot, records[1].Snapshot}
	err = retry.persist()
	retry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(records)
	retry.Close()

	c.Engine = newFake()
	repaired := startService(t, c)
	snap := snapshot(t, repaired)
	if len(snap.LegacyHistory) != 0 || len(snap.Tasks) != 1 || snap.Tasks[0].State != "completed" {
		t.Fatalf("retry became duplicate legacy: %+v", snap)
	}
	afterRecords, err := repaired.store.History()
	after, _ := json.Marshal(afterRecords)
	if err != nil || string(before) != string(after) {
		t.Fatal("recovery changed original attempt journals")
	}
	data, err := os.ReadFile(filepath.Join(c.StateRoot, "desktop-queue.json"))
	var saved queueFile
	if err != nil || json.Unmarshal(data, &saved) != nil || len(saved.LegacyHistory) != 0 {
		t.Fatal("legacy repair not persisted")
	}
	if got := archive(t, repaired); got != 1 {
		t.Fatal("archive count", got)
	}
	repaired.Close()

	c.Engine = newFake()
	archived := startService(t, c)
	if snap = snapshot(t, archived); len(snap.Tasks) != 0 || len(snap.LegacyHistory) != 0 {
		t.Fatalf("archived retry reimported: %+v", snap)
	}
	if replay := enqueue(t, archived, request); replay.ID != task.ID || replay.State != "completed" {
		t.Fatal("archive lost original receipt")
	}
	finalRecords, _ := archived.store.History()
	final, _ := json.Marshal(finalRecords)
	if string(before) != string(final) {
		t.Fatal("archive/recovery changed journals")
	}
}

func TestGenuineLegacyPendingIsHistoricalAndSurvivesRotation(t *testing.T) {
	c := fixtureConfig(t, newFake())
	store, err := storage.Open(c.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.InitializeOwnership(); err != nil {
		t.Fatal(err)
	}
	original := contracts.JobSnapshot{HostSessionID: "legacy-session", JobID: "legacy-job", StartRequestID: "legacy-request", State: contracts.StateDownloading, Progress: &contracts.Progress{Stage: contracts.StateDownloading}}
	if err = store.Put(storage.Record{Fingerprint: "legacy", Snapshot: original}); err != nil {
		t.Fatal(err)
	}
	s := startService(t, c)
	legacy := snapshot(t, s).LegacyHistory
	if len(legacy) != 1 || legacy[0].State != contracts.StateInterrupted || legacy[0].Progress != nil || legacy[0].Error == nil {
		t.Fatalf("legacy appears live: %+v", legacy)
	}
	if err = validateSavedSnapshot(legacy[0]); err != nil {
		t.Fatal(err)
	}
	records, err := store.History()
	if err != nil || len(records) != 1 || records[0].Snapshot.State != contracts.StateDownloading {
		t.Fatal("original legacy journal modified")
	}
	s.Close()
	if err = store.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	c.Engine = newFake()
	reopened := startService(t, c)
	legacy = snapshot(t, reopened).LegacyHistory
	if len(legacy) != 1 || legacy[0].StartRequestID != original.StartRequestID || legacy[0].State != contracts.StateInterrupted {
		t.Fatalf("genuine legacy lost: %+v", legacy)
	}
}
