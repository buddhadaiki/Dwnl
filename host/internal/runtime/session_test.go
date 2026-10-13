package runtime

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/storage"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testExtensionID = "lphdonkmiecidfgjfplhdfnpphmdioeh"

type blockingEngine struct {
	begun chan struct{}
	done  chan struct{}
	once  sync.Once
}

func (e *blockingEngine) Analyze(ctx context.Context, url string) (contracts.Analysis, error) {
	return contracts.Analysis{AnalysisID: "analysis-a", SourceURL: url, ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), Items: []contracts.MediaItem{{ItemID: "item-a", Title: "Local test", HasAudio: true, VideoChoices: []contracts.VideoChoice{{OptionID: "video-a", Container: "mp4", VideoCodec: "h264", HasAudio: true, AudioCodec: "aac"}}}}}, nil
}
func (e *blockingEngine) Execute(ctx context.Context, p contracts.DownloadPlan, progress func(contracts.Progress)) (contracts.CompletedFile, error) {
	e.once.Do(func() { close(e.begun) })
	<-ctx.Done()
	if e.done != nil {
		<-e.done
	}
	return contracts.CompletedFile{}, ctx.Err()
}
func newTestSession(t *testing.T, engine ports.Engine, storeRoot string) *Session {
	t.Helper()
	s, err := storage.Open(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	d, err := storage.NewDestinations(filepath.Join(filepath.Dir(storeRoot), "downloads"), func(context.Context) (string, bool, error) { return "", true, nil })
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(Options{SessionID: NewID(), Engine: engine, Store: s, Destinations: d, Origin: "chrome-extension://" + testExtensionID + "/", ExtensionID: testExtensionID, Handshake: contracts.HandshakeResult{HostVersion: "0.1.0", EngineVersion: "0.1.0", Dependencies: []contracts.Dependency{}, Capabilities: contracts.Capabilities{}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	response := session.Handle(context.Background(), request("hello", "", contracts.OperationHandshake, contracts.HandshakePayload{ExtensionVersion: "0.1.0"}))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	return session
}
func request(id, session string, op contracts.Operation, payload any) contracts.Request {
	b, _ := json.Marshal(payload)
	return contracts.Request{ProtocolVersion: 1, RequestID: id, HostSessionID: session, Operation: op, Payload: b}
}
func analyzeStart(t *testing.T, s *Session, id string) contracts.Request {
	t.Helper()
	a := s.Handle(context.Background(), request("analyze-"+id, s.ID(), contracts.OperationAnalyze, contracts.AnalyzePayload{URL: "https://www.youtube.com/watch?v=fixture"}))
	if a.Error != nil {
		t.Fatal(a.Error)
	}
	return request(id, s.ID(), contracts.OperationStart, contracts.StartPayload{AnalysisID: "analysis-a", ItemID: "item-a", Mode: contracts.ModeMP3, BitrateKbps: 192, DestinationID: s.options.Destinations.Default().DestinationID})
}
func TestStartIdempotentByRequestIDAndConflict(t *testing.T) {
	e := &blockingEngine{begun: make(chan struct{})}
	s := newTestSession(t, e, filepath.Join(t.TempDir(), "state"))
	req := analyzeStart(t, s, "start-a")
	first := s.Handle(context.Background(), req)
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	second := s.Handle(context.Background(), req)
	if second.Error != nil || string(first.Result) != string(second.Result) {
		t.Fatalf("duplicate start %s %v", second.Result, second.Error)
	}
	altered := req
	altered.Payload = json.RawMessage(`{"analysisId":"analysis-a","itemId":"item-a","mode":"mp3","bitrateKbps":320,"destinationId":"different"}`)
	if got := s.Handle(context.Background(), altered); got.Error == nil || got.Error.Code != contracts.RequestConflict {
		t.Fatalf("changed request = %+v", got)
	}
	start, _ := contracts.ResultAs[contracts.StartResult](first)
	status := s.Handle(context.Background(), request("status-a", s.ID(), contracts.OperationStatus, contracts.StatusPayload{StartRequestID: "start-a"}))
	result, _ := contracts.ResultAs[contracts.StatusResult](status)
	if result.Snapshot == nil || result.Snapshot.JobID != start.JobID {
		t.Fatal("lost start ack cannot be resolved")
	}
}
func TestBusyAcrossConnections(t *testing.T) {
	base := t.TempDir()
	first := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(base, "a", "state"))
	second := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(base, "b", "state"))
	reqA := analyzeStart(t, first, "start-a")
	reqB := analyzeStart(t, second, "start-b")
	if got := first.Handle(context.Background(), reqA); got.Error != nil {
		t.Fatal(got.Error)
	}
	if got := second.Handle(context.Background(), reqB); got.Error == nil || got.Error.Code != contracts.Busy {
		t.Fatalf("second connection = %+v", got)
	}
}
func TestCancelRemainsCancelingUntilEngineReturns(t *testing.T) {
	e := &blockingEngine{begun: make(chan struct{}), done: make(chan struct{})}
	s := newTestSession(t, e, filepath.Join(t.TempDir(), "state"))
	start := s.Handle(context.Background(), analyzeStart(t, s, "start-a"))
	result, _ := contracts.ResultAs[contracts.StartResult](start)
	<-e.begun
	response := s.Handle(context.Background(), request("cancel-a", s.ID(), contracts.OperationCancel, contracts.CancelPayload{JobID: result.JobID}))
	cancel, _ := contracts.ResultAs[contracts.CancelResult](response)
	if cancel.Snapshot.State != contracts.StateCanceling {
		close(e.done)
		t.Fatalf("premature terminal %s", cancel.Snapshot.State)
	}
	close(e.done)
	waitState(t, s, result.JobID, contracts.StateCanceled)
}
func waitState(t *testing.T, s *Session, job string, want contracts.JobState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := s.Handle(context.Background(), request(NewID(), s.ID(), contracts.OperationStatus, contracts.StatusPayload{JobID: job}))
		result, _ := contracts.ResultAs[contracts.StatusResult](response)
		if result.Snapshot != nil && result.Snapshot.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("terminal state not reached")
}
func TestReconcileOldSessionReadOnlyNoRestart(t *testing.T) {
	base := t.TempDir()
	storeRoot := filepath.Join(base, "state")
	e := &blockingEngine{begun: make(chan struct{})}
	old := newTestSession(t, e, storeRoot)
	start := old.Handle(context.Background(), analyzeStart(t, old, "old-start"))
	startResult, _ := contracts.ResultAs[contracts.StartResult](start)
	old.Close()
	before, err := os.ReadFile(filepath.Join(storeRoot, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	current := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, storeRoot)
	response := current.Handle(context.Background(), request("reconcile-a", current.ID(), contracts.OperationReconcile, contracts.ReconcilePayload{PreviousHostSessionID: old.ID(), JobID: startResult.JobID}))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	result, _ := contracts.ResultAs[contracts.ReconcileResult](response)
	if result.Outcome != "confirmed" || result.Snapshot == nil || result.Snapshot.State != contracts.StateInterrupted {
		t.Fatalf("reconcile = %+v", result)
	}
	after, _ := os.ReadFile(filepath.Join(storeRoot, "history.json"))
	if string(before) != string(after) {
		t.Fatal("read-only reconcile changed ledger")
	}
	if got := current.Handle(context.Background(), request("wrong-session", old.ID(), contracts.OperationStatus, contracts.StatusPayload{JobID: startResult.JobID})); got.Error == nil || got.Error.Code != contracts.SessionEnded {
		t.Fatal("old session status accepted")
	}
}
func TestOriginAndStrictHandleRouting(t *testing.T) {
	if _, err := NewSession(Options{Origin: "chrome-extension://other/", ExtensionID: testExtensionID}); err == nil {
		t.Fatal("foreign origin accepted")
	}
	s := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(t.TempDir(), "state"))
	bad := request("bad", s.ID(), contracts.OperationStatus, map[string]string{"command": "cmd.exe"})
	if got := s.Handle(context.Background(), bad); got.Error == nil || got.Error.Code != contracts.InvalidMessage {
		t.Fatalf("extra payload property accepted: %+v", got)
	}
}

type failedCancelEngine struct{ blockingEngine }

func (e *failedCancelEngine) Execute(ctx context.Context, p contracts.DownloadPlan, progress func(contracts.Progress)) (contracts.CompletedFile, error) {
	os.WriteFile(filepath.Join(p.WorkDir, "possibly-in-use.bin"), []byte("owned but active"), 0600)
	<-ctx.Done()
	return contracts.CompletedFile{}, &contracts.ProtocolError{Code: contracts.CancelFailed, Message: "tree termination is unconfirmed"}
}
func TestUnconfirmedTerminationNeverClaimsCanceled(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	dest, err := storage.NewDestinations(filepath.Join(base, "downloads"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{SessionID: NewID(), Engine: &failedCancelEngine{}, Store: store, Destinations: dest, Origin: "chrome-extension://" + testExtensionID + "/", ExtensionID: testExtensionID, Handshake: contracts.HandshakeResult{HostVersion: "0.1.0", EngineVersion: "0.1.0", Dependencies: []contracts.Dependency{}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.Handle(context.Background(), request("hello", "", contracts.OperationHandshake, contracts.HandshakePayload{ExtensionVersion: "0.1.0"}))
	response := s.Handle(context.Background(), analyzeStart(t, s, "start-a"))
	start, _ := contracts.ResultAs[contracts.StartResult](response)
	s.Handle(context.Background(), request("cancel-a", s.ID(), contracts.OperationCancel, contracts.CancelPayload{JobID: start.JobID}))
	waitState(t, s, start.JobID, contracts.StateFailed)
	record, err := store.Find(s.ID(), start.JobID, "")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.TerminalConfirmed || record.Snapshot.Error == nil || record.Snapshot.Error.Code != contracts.CancelFailed {
		t.Fatalf("unconfirmed termination persisted as success: %+v", record)
	}
	if _, err = os.Stat(filepath.Join(store.Root(), "tasks", start.JobID, "possibly-in-use.bin")); err != nil {
		t.Fatalf("cleaned a task whose process tree could still be using its files: %v", err)
	}
	if got := s.Handle(context.Background(), request("analyze-another", s.ID(), contracts.OperationAnalyze, contracts.AnalyzePayload{URL: "https://www.youtube.com/watch?v=fixture"})); got.Error == nil || got.Error.Code != contracts.Busy {
		t.Fatal("unconfirmed active tree released user task lock")
	}
}
func TestReconcileCompletedRequiresUnchangedPhysicalFile(t *testing.T) {
	base := t.TempDir()
	s := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(base, "state"))
	path := filepath.Join(base, "finished.mp4")
	if err := os.WriteFile(path, []byte("verified media"), 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("verified media"))
	completed := contracts.CompletedFile{Path: path, Format: "mp4", SizeBytes: 14, SHA256: hex.EncodeToString(hash[:]), VideoCodec: "h264", AudioCodec: "aac"}
	record := storage.Record{Fingerprint: "start-digest", TerminalConfirmed: true, Snapshot: contracts.JobSnapshot{HostSessionID: "previous-session", JobID: "previous-job", StartRequestID: "previous-start", Sequence: 3, State: contracts.StateCompleted, CompletedFile: &completed}}
	if err := s.options.Store.Put(record); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(base, "state", "history.json"))
	query := func(id string) contracts.ReconcileResult {
		response := s.Handle(context.Background(), request(id, s.ID(), contracts.OperationReconcile, contracts.ReconcilePayload{PreviousHostSessionID: "previous-session", StartRequestID: "previous-start"}))
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		result, err := contracts.ResultAs[contracts.ReconcileResult](response)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if got := query("reconcile-original"); got.Outcome != "confirmed" || got.Snapshot == nil || got.Snapshot.State != contracts.StateCompleted {
		t.Fatalf("valid old completion = %+v", got)
	}
	if err := os.WriteFile(path, []byte("tampered media"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := query("reconcile-tampered"); got.Outcome != "unconfirmed" || got.Snapshot == nil || got.Snapshot.State != contracts.StateInterrupted || got.Snapshot.CompletedFile != nil {
		t.Fatalf("tampered old completion = %+v", got)
	}
	after, _ := os.ReadFile(filepath.Join(base, "state", "history.json"))
	if string(before) != string(after) {
		t.Fatal("physical reconciliation modified the record")
	}
}
func TestStartRejectsUnapprovedDestinationBeforeCreatingTask(t *testing.T) {
	s := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(t.TempDir(), "state"))
	req := analyzeStart(t, s, "start-unapproved")
	payload, _ := contracts.PayloadAs[contracts.StartPayload](req)
	payload.DestinationID = "unapproved-folder"
	req.Payload, _ = json.Marshal(payload)
	response := s.Handle(context.Background(), req)
	if response.Error == nil || response.Error.Code != contracts.DestinationError {
		t.Fatalf("unapproved destination = %+v", response)
	}
	if _, err := os.Stat(s.options.Store.Root()); !os.IsNotExist(err) {
		t.Fatalf("unapproved destination created owned task state: %v", err)
	}
}

type observedCancellationEngine struct {
	blockingEngine
	stopped chan struct{}
}

func (e *observedCancellationEngine) Execute(ctx context.Context, _ contracts.DownloadPlan, _ func(contracts.Progress)) (contracts.CompletedFile, error) {
	close(e.begun)
	<-ctx.Done()
	close(e.stopped)
	return contracts.CompletedFile{}, ctx.Err()
}
func TestCorruptLedgerCannotPreventCancelingActiveTask(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	dest, err := storage.NewDestinations(filepath.Join(base, "downloads"), nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &observedCancellationEngine{blockingEngine: blockingEngine{begun: make(chan struct{})}, stopped: make(chan struct{})}
	s, err := NewSession(Options{SessionID: NewID(), Engine: e, Store: store, Destinations: dest, Origin: "chrome-extension://" + testExtensionID + "/", ExtensionID: testExtensionID, Handshake: contracts.HandshakeResult{HostVersion: "0.1.0", EngineVersion: "0.1.0", Dependencies: []contracts.Dependency{}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.Handle(context.Background(), request("hello", "", contracts.OperationHandshake, contracts.HandshakePayload{ExtensionVersion: "0.1.0"}))
	response := s.Handle(context.Background(), analyzeStart(t, s, "start-a"))
	start, _ := contracts.ResultAs[contracts.StartResult](response)
	<-e.begun
	if err = os.WriteFile(filepath.Join(store.Root(), "history.json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	response = s.Handle(context.Background(), request("cancel-a", s.ID(), contracts.OperationCancel, contracts.CancelPayload{JobID: start.JobID}))
	if response.Error == nil || response.Error.Code != contracts.InternalError {
		t.Fatalf("corrupt ledger cancel result = %+v", response)
	}
	select {
	case <-e.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("ledger failure left the active task running after cancel")
	}
	waitState(t, s, start.JobID, contracts.StateFailed)
}

type publishedBeforeReturnEngine struct {
	blockingEngine
	published   chan contracts.CompletedFile
	allowReturn chan struct{}
}

func (e *publishedBeforeReturnEngine) Execute(_ context.Context, plan contracts.DownloadPlan, _ func(contracts.Progress)) (contracts.CompletedFile, error) {
	// The slow/external engine boundary pauses only after its real final file is
	// durable. The runtime must decide the resulting state from that proof.
	data := []byte("verified completed output")
	path := filepath.Join(plan.DestinationPath, "confirmed-"+plan.JobID+".mp3")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return contracts.CompletedFile{}, err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return contracts.CompletedFile{}, err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return contracts.CompletedFile{}, err
	}
	if err = file.Close(); err != nil {
		return contracts.CompletedFile{}, err
	}
	hash := sha256.Sum256(data)
	completed := contracts.CompletedFile{Path: path, Format: "mp3", SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(hash[:]), AudioCodec: "mp3", MP3BitrateKbps: 192}
	e.published <- completed
	<-e.allowReturn
	return completed, nil
}

func TestConfirmedCompletionWinsLateCancelOrDisconnect(t *testing.T) {
	for _, trigger := range []string{"cancel", "disconnect"} {
		t.Run(trigger, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "state")
			engine := &publishedBeforeReturnEngine{published: make(chan contracts.CompletedFile, 1), allowReturn: make(chan struct{})}
			session := newTestSession(t, engine, root)
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(engine.allowReturn) }) })
			response := session.Handle(context.Background(), analyzeStart(t, session, "confirmed-start"))
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			start, err := contracts.ResultAs[contracts.StartResult](response)
			if err != nil {
				t.Fatal(err)
			}
			var completed contracts.CompletedFile
			select {
			case completed = <-engine.published:
			case <-time.After(5 * time.Second):
				t.Fatal("engine did not publish its real final file")
			}
			if err := storage.VerifyCompleted(completed); err != nil {
				t.Fatal(err)
			}
			var closed chan struct{}
			if trigger == "cancel" {
				response = session.Handle(context.Background(), request("late-cancel", session.ID(), contracts.OperationCancel, contracts.CancelPayload{JobID: start.JobID}))
				if response.Error != nil {
					t.Fatal(response.Error)
				}
				cancel, err := contracts.ResultAs[contracts.CancelResult](response)
				if err != nil {
					t.Fatal(err)
				}
				if cancel.Snapshot.State != contracts.StateCanceling {
					t.Fatalf("engine still paused, cancel state = %s", cancel.Snapshot.State)
				}
			} else {
				closed = make(chan struct{})
				go func() { session.Close(); close(closed) }()
				// Wait for Close to cancel this session before allowing success to return.
				select {
				case <-session.context.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("session close did not begin")
				}
			}
			release.Do(func() { close(engine.allowReturn) })
			session.wg.Wait()
			if closed != nil {
				select {
				case <-closed:
				case <-time.After(5 * time.Second):
					t.Fatal("session close did not complete")
				}
			}
			record, err := session.options.Store.Find(session.ID(), start.JobID, "")
			if err != nil {
				t.Fatal(err)
			}
			if record == nil || record.Snapshot.State != contracts.StateCompleted || record.Snapshot.CompletedFile == nil || !record.TerminalConfirmed {
				t.Fatalf("%s after physical publication lost confirmed completion: %+v", trigger, record)
			}
			if record.Snapshot.CompletedFile.Path != completed.Path {
				t.Fatal("completion persisted a different final path")
			}
			if got, err := os.ReadFile(completed.Path); err != nil || string(got) != "verified completed output" {
				t.Fatalf("final file was removed or changed: %q, %v", got, err)
			}
			if trigger == "cancel" {
				response = session.Handle(context.Background(), request("status-completed", session.ID(), contracts.OperationStatus, contracts.StatusPayload{JobID: start.JobID}))
				status, _ := contracts.ResultAs[contracts.StatusResult](response)
				if status.Snapshot == nil || status.Snapshot.State != contracts.StateCompleted {
					t.Fatal("live status does not preserve confirmed completion")
				}
				session.Close()
			}
			current := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, root)
			response = current.Handle(context.Background(), request("reconcile-completed", current.ID(), contracts.OperationReconcile, contracts.ReconcilePayload{PreviousHostSessionID: session.ID(), JobID: start.JobID}))
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			reconciled, _ := contracts.ResultAs[contracts.ReconcileResult](response)
			if reconciled.Outcome != "confirmed" || reconciled.Snapshot == nil || reconciled.Snapshot.State != contracts.StateCompleted || reconciled.Snapshot.CompletedFile == nil {
				t.Fatalf("late %s completion did not reconcile: %+v", trigger, reconciled)
			}
		})
	}
}
