package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/storage"
	"dwnl/host/internal/testutil"
	hostwin "dwnl/host/internal/windows"
)

type fakeEngine struct {
	mu                                 sync.Mutex
	count, active, maxActive, executed int
	gate                               chan struct{}
	started                            chan string
	probeGate                          chan struct{}
	changed                            bool
	identities                         map[string]string
}

func newFake() *fakeEngine {
	return &fakeEngine{gate: make(chan struct{}, 20), started: make(chan string, 20), identities: map[string]string{}}
}
func (f *fakeEngine) Probe(ctx context.Context) error {
	if f.probeGate != nil {
		select {
		case <-f.probeGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (f *fakeEngine) Capabilities() contracts.Capabilities {
	return contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}
}
func (f *fakeEngine) Dependencies() []contracts.Dependency { return nil }
func (f *fakeEngine) Analyze(ctx context.Context, url string) (contracts.Analysis, error) {
	if err := ctx.Err(); err != nil {
		return contracts.Analysis{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	id := fmt.Sprintf("analysis-%d", f.count)
	item := fmt.Sprintf("item-%d", f.count)
	option := fmt.Sprintf("choice-%d", f.count)
	identity := "source-stream"
	if f.changed {
		identity = "other-stream"
	}
	sum := sha256.Sum256([]byte(identity))
	f.identities[id+":"+item+":"+option] = hex.EncodeToString(sum[:])
	f.identities[id+":"+item+":"] = hex.EncodeToString(sum[:])
	return contracts.Analysis{AnalysisID: id, SourceURL: url, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Items: []contracts.MediaItem{{ItemID: item, Title: "Fixture", HasAudio: true, VideoChoices: []contracts.VideoChoice{{OptionID: option, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", HasAudio: true}}}}}, nil
}
func (f *fakeEngine) SelectionIdentity(a, i, o string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.identities[a+":"+i+":"+o]
	if !ok {
		return "", errors.New("expired")
	}
	return id, nil
}
func (f *fakeEngine) Execute(ctx context.Context, p contracts.DownloadPlan, emit func(contracts.Progress)) (contracts.CompletedFile, error) {
	f.mu.Lock()
	f.active++
	f.executed++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	f.started <- p.RequestID
	select {
	case <-f.gate:
	case <-ctx.Done():
		return contracts.CompletedFile{}, ctx.Err()
	}
	data := []byte("real verified fixture bytes")
	sum := sha256.Sum256(data)
	path := filepath.Join(p.DestinationPath, p.JobID+".mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return contracts.CompletedFile{}, err
	}
	emit(contracts.Progress{Stage: contracts.StateSaving})
	return contracts.CompletedFile{Path: path, Format: "mp4", SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:]), VideoCodec: "h264", AudioCodec: "aac"}, nil
}
func fixtureConfig(t *testing.T, f *fakeEngine) Config {
	t.Helper()
	root := testutil.TempDir(t)
	dest := filepath.Join(root, "downloads")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	return Config{StateRoot: filepath.Join(root, "state"), DefaultDestination: dest, Engine: f}
}
func startService(t *testing.T, c Config) *Service {
	t.Helper()
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitFor(t, func() bool { return snapshot(t, s).Readiness != "preparing" })
	return s
}
func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func snapshot(t *testing.T, s *Service) Snapshot {
	t.Helper()
	v, err := s.Dispatch(context.Background(), "snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	return v.(Snapshot)
}
func selection(t *testing.T, s *Service, id string) enqueuePayload {
	t.Helper()
	v, err := s.Dispatch(context.Background(), "analyze", json.RawMessage(`{"url":"https://www.youtube.com/shorts/owned"}`))
	if err != nil {
		t.Fatal(err)
	}
	a := v.(contracts.Analysis)
	return enqueuePayload{RequestID: id, StartPayload: contracts.StartPayload{AnalysisID: a.AnalysisID, ItemID: a.Items[0].ItemID, Mode: contracts.ModeVideo, VideoOptionID: a.Items[0].VideoChoices[0].OptionID, DestinationID: snapshot(t, s).Destination.DestinationID}}
}
func enqueue(t *testing.T, s *Service, p enqueuePayload) Task {
	t.Helper()
	b, _ := json.Marshal(p)
	v, err := s.Dispatch(context.Background(), "enqueue", b)
	if err != nil {
		t.Fatal(err)
	}
	return v.(Task)
}
func state(t *testing.T, s *Service, id string) string {
	t.Helper()
	for _, task := range snapshot(t, s).Tasks {
		if task.ID == id {
			return task.State
		}
	}
	t.Fatal("task missing")
	return ""
}
func command(t *testing.T, s *Service, method, payload string) {
	t.Helper()
	if _, err := s.Dispatch(context.Background(), method, json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotDoesNotWaitForProbe(t *testing.T) {
	f := newFake()
	f.probeGate = make(chan struct{})
	c := fixtureConfig(t, f)
	started := time.Now()
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := snapshot(t, s)
	if snap.Readiness != "preparing" || snap.Tasks == nil || snap.Dependencies == nil {
		t.Fatalf("snapshot: %+v", snap)
	}
	if time.Since(started) > time.Second {
		t.Fatal("initial connection waited for probe")
	}
	close(f.probeGate)
	waitFor(t, func() bool { return snapshot(t, s).Readiness == "ready" })
}
func TestSequentialQueueAndDurableIdempotency(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	p := selection(t, s, "first")
	first := enqueue(t, s, p)
	if got := enqueue(t, s, p); got.ID != first.ID {
		t.Fatal("duplicate task")
	}
	second := enqueue(t, s, selection(t, s, "second"))
	select {
	case got := <-f.started:
		if got != "first" {
			t.Fatalf("order %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first did not start")
	}
	select {
	case <-f.started:
		t.Fatal("parallel download")
	case <-time.After(30 * time.Millisecond):
	}
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, first.ID) == "completed" })
	select {
	case got := <-f.started:
		if got != "second" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second did not start")
	}
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, second.ID) == "completed" })
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := enqueue(t, s2, p); got.ID != first.ID || got.State != "completed" {
		t.Fatal("durable replay failed")
	}
	p.BitrateKbps = 192
	b, _ := json.Marshal(p)
	if _, err := s2.Dispatch(context.Background(), "enqueue", b); err == nil {
		t.Fatal("conflicting request accepted")
	}
	if len(snapshot(t, s2).Tasks) != 2 {
		t.Fatal("duplicate after restart")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.maxActive != 1 {
		t.Fatal("max active", f.maxActive)
	}
}
func TestCloseReopenRequiresContinueAndPreservesOrder(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	first := enqueue(t, s, selection(t, s, "first"))
	second := enqueue(t, s, selection(t, s, "second"))
	waitStarted(t, f)
	s.Close()
	f2 := newFake()
	c.Engine = f2
	s2 := startService(t, c)
	snap := snapshot(t, s2)
	if !snap.Paused || state(t, s2, first.ID) != "interrupted" || state(t, s2, second.ID) != "queued" {
		t.Fatalf("restart %+v", snap)
	}
	select {
	case <-f2.started:
		t.Fatal("automatically resumed")
	case <-time.After(30 * time.Millisecond):
	}
	command(t, s2, "continue", "{}")
	select {
	case got := <-f2.started:
		if got != "first" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("continue did not start")
	}
	f2.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s2, first.ID) == "completed" })
	command(t, s2, "cancel", `{"id":"`+second.ID+`"}`)
	waitFor(t, func() bool { return state(t, s2, second.ID) == "canceled" })
}
func TestCancelPendingAtExitIsNotRequeued(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	first := enqueue(t, s, selection(t, s, "first"))
	waitStarted(t, f)
	// Exit wins the race against the canceled process: the task is still canceling.
	s.mu.Lock()
	s.tasks[s.taskIndex(first.ID)].Task.State = "canceling"
	s.mu.Unlock()
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := state(t, s2, first.ID); got != "canceled" {
		t.Fatalf("canceled download restored as %q", got)
	}
}
func TestQueuedAndActiveCancelDoNotAffectOtherTask(t *testing.T) {
	f := newFake()
	s := startService(t, fixtureConfig(t, f))
	first := enqueue(t, s, selection(t, s, "first"))
	waitStarted(t, f)
	second := enqueue(t, s, selection(t, s, "second"))
	third := enqueue(t, s, selection(t, s, "third"))
	command(t, s, "cancel", `{"id":"`+second.ID+`"}`)
	if state(t, s, second.ID) != "canceled" {
		t.Fatal("queued cancel")
	}
	command(t, s, "cancel", `{"id":"`+first.ID+`"}`)
	waitFor(t, func() bool { return state(t, s, first.ID) == "canceled" })
	select {
	case got := <-f.started:
		if got != "third" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("third did not start")
	}
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, third.ID) == "completed" })
}
func TestChangedSelectionNeverDownloadsFallback(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	task := enqueue(t, s, selection(t, s, "original"))
	s.Close()
	f2 := newFake()
	f2.changed = true
	c.Engine = f2
	s2 := startService(t, c)
	command(t, s2, "continue", "{}")
	waitFor(t, func() bool { return state(t, s2, task.ID) == "failed" })
	if !strings.Contains(snapshot(t, s2).Tasks[0].Error, "formato selecionado mudou") {
		t.Fatal(snapshot(t, s2))
	}
	f2.mu.Lock()
	defer f2.mu.Unlock()
	if f2.executed != 0 {
		t.Fatal("fallback downloaded")
	}
}
func TestCorruptQueueIsNotOverwritten(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	s.Close()
	path := filepath.Join(c.StateRoot, "desktop-queue.json")
	bad := []byte(`{"version":1,"tasks":`)
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if service, err := New(c); !errors.Is(err, ErrCorruptQueue) {
		if service != nil {
			service.Close()
		}
		t.Fatalf("corruption: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(bad) {
		t.Fatal("corrupted queue overwritten")
	}
}
func TestEnqueuePersistenceFailureHasNoAcceptedTask(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	p := selection(t, s, "not-accepted")
	path := filepath.Join(c.StateRoot, "desktop-queue.json")
	unlock, err := hostwin.LockPath(path, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	_, err = s.Dispatch(context.Background(), "enqueue", b)
	unlock()
	if err == nil {
		t.Skip("platform permits replacing an open file; Windows covers this regression")
	}
	if len(snapshot(t, s).Tasks) != 0 {
		t.Fatal("failed durable append remained accepted")
	}
}
func TestCompletedPublicationReconcilesBeforeManualContinue(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	task := enqueue(t, s, selection(t, s, "published"))
	const job = "crashed-job"
	snapshot := contracts.JobSnapshot{HostSessionID: s.sessionID, JobID: job, StartRequestID: task.RequestID, State: contracts.StateSaving}
	if err := s.store.Put(storage.Record{Fingerprint: "proof", Snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	file, owned, err := storage.CreateIntermediate(c.DefaultDestination, job)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("verified publication")
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	complete := contracts.CompletedFile{Path: filepath.Join(c.DefaultDestination, "published.mp4"), Format: "mp4", SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:]), VideoCodec: "h264", AudioCodec: "aac"}
	proof := contracts.PreparedFinalization{File: complete, Identity: contracts.PhysicalIdentity{VolumeSerialNumber: owned.Identity.VolumeSerialNumber, FileIndexHigh: owned.Identity.FileIndexHigh, FileIndexLow: owned.Identity.FileIndexLow}, ParentIdentity: contracts.PhysicalIdentity{VolumeSerialNumber: owned.ParentIdentity.VolumeSerialNumber, FileIndexHigh: owned.ParentIdentity.FileIndexHigh, FileIndexLow: owned.ParentIdentity.FileIndexLow}}
	if err = s.store.PrepareFinalization(s.sessionID, job, proof); err != nil {
		t.Fatal(err)
	}
	if err = owned.Publish(complete.Path); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tasks[0].AttemptSession = s.sessionID
	s.tasks[0].AttemptJob = job
	s.tasks[0].Task.State = "saving"
	err = s.persist()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	f2 := newFake()
	c.Engine = f2
	s2 := startService(t, c)
	if state(t, s2, task.ID) != "completed" {
		t.Fatal("published output was not reconciled")
	}
	command(t, s2, "continue", "{}")
	select {
	case <-f2.started:
		t.Fatal("duplicate after published output")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestLegacyPreferencesAndHistorySurviveLedgerRotation(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	store, err := storage.Open(c.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.InitializeOwnership(); err != nil {
		t.Fatal(err)
	}
	preferred := filepath.Join(filepath.Dir(c.StateRoot), "preferred")
	if err = os.Mkdir(preferred, 0700); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveDestinationPreference(preferred); err != nil {
		t.Fatal(err)
	}
	legacy := contracts.JobSnapshot{HostSessionID: "old-session", JobID: "old-job", StartRequestID: "old-request", State: contracts.StateCanceled}
	if err = store.Put(storage.Record{Fingerprint: "old-request", Snapshot: legacy, TerminalConfirmed: true}); err != nil {
		t.Fatal(err)
	}
	s := startService(t, c)
	snap := snapshot(t, s)
	if snap.Destination.Label != preferred || len(snap.LegacyHistory) != 1 {
		t.Fatalf("legacy migration: %+v", snap)
	}
	s.Close()
	if err = store.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	c.Engine = newFake()
	s2 := startService(t, c)
	if len(snapshot(t, s2).LegacyHistory) != 1 {
		t.Fatal("legacy history disappeared after original ledger rotation")
	}
}
func TestMissingAttemptJournalCannotDuplicateUnconfirmedPublication(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	task := enqueue(t, s, selection(t, s, "missing-journal"))
	s.mu.Lock()
	s.tasks[0].AttemptSession = "lost-session"
	s.tasks[0].AttemptJob = "lost-job"
	s.tasks[0].Task.State = "saving"
	err := s.persist()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if state(t, s2, task.ID) != "failed" {
		t.Fatal("missing publication journal incorrectly made retryable")
	}
}
func TestProbeTimeoutIsVisibleAndSnapshotRemainsAvailable(t *testing.T) {
	f := newFake()
	f.probeGate = make(chan struct{})
	c := fixtureConfig(t, f)
	c.ProbeTimeout = 30 * time.Millisecond
	s := startService(t, c)
	snap := snapshot(t, s)
	if snap.Readiness != "degraded" || !strings.Contains(snap.Error, "tempo limite") {
		t.Fatalf("timeout: %+v", snap)
	}
}
func TestCallerDisconnectDoesNotCancelAcceptedDownload(t *testing.T) {
	f := newFake()
	s := startService(t, fixtureConfig(t, f))
	p := selection(t, s, "disconnected")
	b, _ := json.Marshal(p)
	ctx, cancel := context.WithCancel(context.Background())
	v, err := s.Dispatch(ctx, "enqueue", b)
	if err != nil {
		t.Fatal(err)
	}
	task := v.(Task)
	cancel()
	waitStarted(t, f)
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, task.ID) == "completed" })
}

func TestQueueLimitPreservesDurableReceipts(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	original := selection(t, s, "receipt")
	first := enqueue(t, s, original)
	s.mu.Lock()
	template := s.tasks[0]
	for len(s.tasks) < maxTasks {
		r := template
		r.Task.ID = fmt.Sprintf("task-%d", len(s.tasks))
		r.Task.RequestID = fmt.Sprintf("receipt-%d", len(s.tasks))
		r.Task.State = "canceled"
		s.tasks = append(s.tasks, r)
	}
	err := s.persist()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := enqueue(t, s, original); got.ID != first.ID {
		t.Fatal("full queue lost idempotency")
	}
	another := selection(t, s, "overflow")
	b, _ := json.Marshal(another)
	if _, err = s.Dispatch(context.Background(), "enqueue", b); err == nil || !strings.Contains(err.Error(), "200 registros") {
		t.Fatalf("capacity: %v", err)
	}
	if len(snapshot(t, s).Tasks) != maxTasks {
		t.Fatal("capacity failure removed existing history")
	}
}

func waitStarted(t *testing.T, f *fakeEngine) string {
	t.Helper()
	select {
	case id := <-f.started:
		return id
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start; check that another Dwnl app is not holding the per-user active-task lease")
		return ""
	}
}
