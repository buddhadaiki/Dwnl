// Package runtime routes current-session requests and owns task lifetimes.
package runtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/storage"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
)

type Options struct {
	SessionID     string
	Engine        ports.Engine
	Store         *storage.Store
	Destinations  *storage.Destinations
	Origin        string
	ExtensionID   string
	Handshake     contracts.HandshakeResult
	LoadHandshake func(context.Context) contracts.HandshakeResult
	Emit          func(contracts.JobEvent)
}
type Session struct {
	options          Options
	mu               sync.Mutex
	context          context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	closed           bool
	handshaken       bool
	handshakeLoading bool
	analyzing        bool
	requests         map[string]string
	analyses         map[string]contracts.Analysis
	analysisOrder    []string
	cursors          map[string]cursor
	jobs             map[string]*job
	starts           map[string]*job
	active           *job
}
type cursor struct {
	analysis, item string
	offset         int
}
type job struct {
	snapshot             contracts.JobSnapshot
	start                contracts.StartResult
	fingerprint          string
	cancel               context.CancelFunc
	lease                *hostwin.Lease
	workDir, destination string
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var extensionPattern = regexp.MustCompile(`^[a-p]{32}$`)

func NewID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(id[:])
}
func NewSession(options Options) (*Session, error) {
	if !extensionPattern.MatchString(options.ExtensionID) || options.Origin != "chrome-extension://"+options.ExtensionID+"/" {
		return nil, errors.New("native origin is not authorized")
	}
	if !idPattern.MatchString(options.SessionID) || options.Engine == nil || options.Store == nil || options.Destinations == nil {
		return nil, errors.New("host configuration is incomplete")
	}
	if options.Handshake.Dependencies == nil {
		options.Handshake.Dependencies = []contracts.Dependency{}
	}
	options.Handshake.DefaultDestination = options.Destinations.Default()
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{options: options, context: ctx, cancel: cancel, requests: map[string]string{}, analyses: map[string]contracts.Analysis{}, cursors: map[string]cursor{}, jobs: map[string]*job{}, starts: map[string]*job{}}, nil
}
func (s *Session) ID() string { return s.options.SessionID }
func (s *Session) Handle(ctx context.Context, req contracts.Request) contracts.Response {
	encoded, err := json.Marshal(req)
	if err != nil {
		return s.failure(req, contracts.InvalidMessage)
	}
	parsed, err := contracts.DecodeRequest(encoded)
	if err != nil {
		var protocol *contracts.ProtocolDecodeError
		if errors.As(err, &protocol) {
			return s.failure(req, protocol.Code)
		}
		return s.failure(req, contracts.InvalidMessage)
	}
	req = parsed
	var canonical any
	json.Unmarshal(encoded, &canonical)
	encoded, _ = json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(sum[:])
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.failure(req, contracts.SessionEnded)
	}
	if req.Operation != contracts.OperationHandshake && (!s.handshaken || req.HostSessionID != s.ID()) {
		s.mu.Unlock()
		return s.failure(req, contracts.SessionEnded)
	}
	if old, found := s.requests[req.RequestID]; found && old != fingerprint {
		s.mu.Unlock()
		return s.failure(req, contracts.RequestConflict)
	}
	if _, found := s.requests[req.RequestID]; !found {
		if len(s.requests) >= 4096 {
			s.mu.Unlock()
			return s.failure(req, contracts.InternalError)
		}
		s.requests[req.RequestID] = fingerprint
	}
	if req.Operation == contracts.OperationHandshake {
		if s.handshaken || s.options.LoadHandshake == nil {
			s.handshaken = true
			result := s.options.Handshake
			s.mu.Unlock()
			return s.success(req, result)
		}
		if s.handshakeLoading {
			s.mu.Unlock()
			return s.failure(req, contracts.Busy)
		}
		s.handshakeLoading = true
		s.wg.Add(1)
		s.mu.Unlock()
		return s.loadHandshake(ctx, req)
	}
	s.mu.Unlock()
	switch req.Operation {
	case contracts.OperationAnalyze:
		return s.analyze(ctx, req)
	case contracts.OperationListFormats:
		return s.listFormats(req)
	case contracts.OperationChooseDestination:
		result, err := s.options.Destinations.Choose(ctx)
		if err != nil {
			return s.failure(req, contracts.DestinationError)
		}
		return s.success(req, result)
	case contracts.OperationStart:
		return s.start(req, fingerprint)
	case contracts.OperationStatus:
		return s.status(req)
	case contracts.OperationReconcile:
		return s.reconcile(req)
	case contracts.OperationCancel:
		return s.cancelJob(req)
	default:
		return s.failure(req, contracts.UnknownOperation)
	}
}
func (s *Session) loadHandshake(ctx context.Context, req contracts.Request) contracts.Response {
	defer s.wg.Done()
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.context, cancel)
	defer stop()
	result := s.options.LoadHandshake(work)
	result.DefaultDestination = s.options.Destinations.Default()
	if result.Dependencies == nil {
		result.Dependencies = []contracts.Dependency{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handshakeLoading = false
	if s.closed || work.Err() != nil {
		return s.failure(req, contracts.SessionEnded)
	}
	response := s.success(req, result)
	if response.Error == nil {
		s.options.Handshake = result
		s.handshaken = true
	}
	return response
}
func (s *Session) success(req contracts.Request, result any) contracts.Response {
	b, err := json.Marshal(result)
	if err != nil {
		return s.failure(req, contracts.InternalError)
	}
	response := contracts.Response{ProtocolVersion: 1, Kind: "response", RequestID: req.RequestID, HostSessionID: s.ID(), Operation: req.Operation, Result: b}
	check, err := json.Marshal(response)
	if err != nil {
		return s.failure(req, contracts.InternalError)
	}
	if _, err = contracts.DecodeMessage(check); err != nil {
		return s.failure(req, contracts.InvalidOutput)
	}
	return response
}
func (s *Session) failure(req contracts.Request, code contracts.ErrorCode) contracts.Response {
	return contracts.Response{ProtocolVersion: 1, Kind: "response", RequestID: req.RequestID, HostSessionID: s.ID(), Operation: req.Operation, Error: safeError(code)}
}
func safeError(code contracts.ErrorCode) *contracts.ProtocolError {
	messages := map[contracts.ErrorCode]string{
		contracts.InvalidMessage: "The control message is invalid", contracts.MessageTooLarge: "The control message is too large", contracts.IncompatibleProtocol: "The protocol version is incompatible", contracts.UnknownOperation: "The operation is unavailable", contracts.InvalidSelection: "The selected media option is invalid", contracts.SessionEnded: "This host session has ended", contracts.AnalysisExpired: "Analyze the source again before starting", contracts.InvalidCursor: "The format cursor is invalid", contracts.Busy: "Another task is active for this Windows user", contracts.RequestConflict: "This request ID was already used for a different request", contracts.UnknownJob: "The task is unknown in this session", contracts.DependencyUnavailable: "A private media dependency is unavailable", contracts.MP3Unavailable: "MP3 encoding is unavailable", contracts.NoAudio: "The source has no audio", contracts.AccessRestricted: "The source requires restricted access", contracts.NetworkError: "The media network request failed", contracts.NetworkPolicyBlocked: "Network media access is blocked by the current policy", contracts.DestinationError: "The selected destination could not be used", contracts.DiskFull: "The destination does not have enough free space", contracts.EngineError: "The media operation failed", contracts.CancelFailed: "The task could not be confirmed as canceled", contracts.InvalidOutput: "The resulting media file could not be verified", contracts.InternalError: "The local host could not complete the operation",
	}
	message, ok := messages[code]
	if !ok {
		code = contracts.InternalError
		message = messages[code]
	}
	return &contracts.ProtocolError{Code: code, Message: message, Retryable: code == contracts.Busy || code == contracts.NetworkError}
}
func errorCode(err error) contracts.ErrorCode {
	var typed *contracts.ProtocolError
	if errors.As(err, &typed) {
		return safeError(typed.Code).Code
	}
	var decode *contracts.ProtocolDecodeError
	if errors.As(err, &decode) {
		return safeError(decode.Code).Code
	}
	return contracts.EngineError
}
func (s *Session) analyze(ctx context.Context, req contracts.Request) contracts.Response {
	payload, _ := contracts.PayloadAs[contracts.AnalyzePayload](req)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.failure(req, contracts.SessionEnded)
	}
	if s.active != nil || s.analyzing {
		s.mu.Unlock()
		return s.failure(req, contracts.Busy)
	}
	lease, busy, err := hostwin.AcquireLease("active-task", false)
	if err != nil {
		s.mu.Unlock()
		return s.failure(req, contracts.InternalError)
	}
	if busy {
		s.mu.Unlock()
		return s.failure(req, contracts.Busy)
	}
	s.analyzing = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	defer lease.Close()
	defer func() { s.mu.Lock(); s.analyzing = false; s.mu.Unlock() }()
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.context, cancel)
	defer stop()
	analysis, err := s.options.Engine.Analyze(work, payload.URL)
	if err != nil {
		if work.Err() != nil {
			return s.failure(req, contracts.SessionEnded)
		}
		return s.failure(req, errorCode(err))
	}
	analysis.HostSessionID = s.ID()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.failure(req, contracts.SessionEnded)
	}
	wire := s.firstPage(analysis)
	response := s.success(req, wire)
	if response.Error != nil {
		return response
	}
	s.analyses[analysis.AnalysisID] = analysis
	s.analysisOrder = append(s.analysisOrder, analysis.AnalysisID)
	if len(s.analysisOrder) > 20 {
		old := s.analysisOrder[0]
		s.analysisOrder = s.analysisOrder[1:]
		delete(s.analyses, old)
		for token, c := range s.cursors {
			if c.analysis == old {
				delete(s.cursors, token)
			}
		}
	}
	return response
}
func (s *Session) firstPage(analysis contracts.Analysis) contracts.Analysis {
	wire := analysis
	wire.Items = append([]contracts.MediaItem(nil), analysis.Items...)
	for i, item := range wire.Items {
		if len(item.VideoChoices) > 100 {
			item.VideoChoices = append([]contracts.VideoChoice(nil), item.VideoChoices[:100]...)
			item.FormatsCursor = NewID()
			item.OmittedMetadata = true
			s.cursors[item.FormatsCursor] = cursor{analysis.AnalysisID, item.ItemID, 100}
		}
		wire.Items[i] = item
	}
	return wire
}
func (s *Session) listFormats(req contracts.Request) contracts.Response {
	payload, _ := contracts.PayloadAs[contracts.ListFormatsPayload](req)
	s.mu.Lock()
	defer s.mu.Unlock()
	analysis, ok := s.analyses[payload.AnalysisID]
	if !ok {
		return s.failure(req, contracts.InvalidSelection)
	}
	if err := contracts.ValidateRequestContext(req, contracts.ValidationContext{HostSessionID: s.ID(), Analysis: &analysis}); err != nil {
		return s.failure(req, errorCode(err))
	}
	for _, item := range analysis.Items {
		if item.ItemID != payload.ItemID {
			continue
		}
		offset := 0
		if payload.Cursor != "" {
			c, ok := s.cursors[payload.Cursor]
			if !ok || c.analysis != payload.AnalysisID || c.item != payload.ItemID {
				return s.failure(req, contracts.InvalidCursor)
			}
			offset = c.offset
		}
		if offset > len(item.VideoChoices) {
			return s.failure(req, contracts.InvalidCursor)
		}
		end := offset + 100
		if end > len(item.VideoChoices) {
			end = len(item.VideoChoices)
		}
		result := contracts.ListFormatsResult{AnalysisID: analysis.AnalysisID, ItemID: item.ItemID, Choices: append([]contracts.VideoChoice{}, item.VideoChoices[offset:end]...), OmittedMetadata: item.OmittedMetadata}
		if end < len(item.VideoChoices) {
			result.NextCursor = NewID()
			s.cursors[result.NextCursor] = cursor{analysis.AnalysisID, item.ItemID, end}
		}
		return s.success(req, result)
	}
	return s.failure(req, contracts.InvalidSelection)
}
func (s *Session) start(req contracts.Request, fingerprint string) contracts.Response {
	payload, _ := contracts.PayloadAs[contracts.StartPayload](req)
	s.mu.Lock()
	if previous, ok := s.starts[req.RequestID]; ok {
		s.mu.Unlock()
		return s.success(req, previous.start)
	}
	if s.closed {
		s.mu.Unlock()
		return s.failure(req, contracts.SessionEnded)
	}
	analysis, ok := s.analyses[payload.AnalysisID]
	if !ok {
		s.mu.Unlock()
		return s.failure(req, contracts.InvalidSelection)
	}
	if err := contracts.ValidateRequestContext(req, contracts.ValidationContext{HostSessionID: s.ID(), Analysis: &analysis}); err != nil {
		s.mu.Unlock()
		return s.failure(req, errorCode(err))
	}
	if s.active != nil || s.analyzing {
		s.mu.Unlock()
		return s.failure(req, contracts.Busy)
	}
	lease, busy, err := hostwin.AcquireLease("active-task", false)
	if err != nil {
		s.mu.Unlock()
		return s.failure(req, contracts.InternalError)
	}
	if busy {
		s.mu.Unlock()
		return s.failure(req, contracts.Busy)
	}
	destination, err := s.options.Destinations.Resolve(payload.DestinationID)
	if err != nil {
		lease.Close()
		s.mu.Unlock()
		return s.failure(req, contracts.DestinationError)
	}
	jobID := NewID()
	workDir, err := s.options.Store.PrepareTask(jobID)
	if err != nil {
		lease.Close()
		s.mu.Unlock()
		return s.failure(req, contracts.InternalError)
	}
	work, cancel := context.WithCancel(s.context)
	j := &job{snapshot: contracts.JobSnapshot{HostSessionID: s.ID(), JobID: jobID, StartRequestID: req.RequestID, Sequence: 0, State: contracts.StateDownloading}, fingerprint: fingerprint, cancel: cancel, lease: lease, workDir: workDir, destination: destination}
	j.start = contracts.StartResult{JobID: jobID, StartRequestID: req.RequestID, Sequence: 0, State: contracts.StateDownloading}
	if err = s.options.Store.Put(storage.Record{Fingerprint: fingerprint, Snapshot: j.snapshot}); err != nil {
		cancel()
		s.options.Store.CleanupTask(jobID)
		lease.Close()
		s.mu.Unlock()
		return s.failure(req, contracts.InternalError)
	}
	var item contracts.MediaItem
	for _, candidate := range analysis.Items {
		if candidate.ItemID == payload.ItemID {
			item = candidate
			break
		}
	}
	plan := contracts.DownloadPlan{RequestID: req.RequestID, HostSessionID: s.ID(), JobID: jobID, Analysis: analysis, Item: item, Mode: payload.Mode, MP3BitrateKbps: payload.BitrateKbps, DestinationID: payload.DestinationID, DestinationPath: destination, WorkDir: workDir}
	if payload.Mode == contracts.ModeVideo {
		for _, choice := range item.VideoChoices {
			if choice.OptionID == payload.VideoOptionID {
				copy := choice
				plan.VideoChoice = &copy
				break
			}
		}
	}
	s.jobs[jobID] = j
	s.starts[req.RequestID] = j
	s.active = j
	s.wg.Add(1)
	s.mu.Unlock()
	go s.execute(work, j, plan)
	return s.success(req, j.start)
}
func (s *Session) execute(ctx context.Context, j *job, plan contracts.DownloadPlan) {
	defer s.wg.Done()
	defer j.cancel()
	completed, err := s.options.Engine.Execute(ctx, plan, func(progress contracts.Progress) { s.progress(j, progress) })
	terminationConfirmed := !(err != nil && errorCode(err) == contracts.CancelFailed)
	if err == nil {
		if !hostwin.IsWithin(j.destination, completed.Path) || storage.VerifyCompleted(completed) != nil {
			err = safeError(contracts.InvalidOutput)
		}
	}
	var cleanupErr error
	if terminationConfirmed {
		cleanupErr = s.options.Store.CleanupTask(j.snapshot.JobID)
	}
	s.mu.Lock()
	snapshot := j.snapshot
	snapshot.Sequence++
	snapshot.Progress = nil
	snapshot.CompletedFile = nil
	snapshot.Error = nil
	switch {
	case !terminationConfirmed:
		snapshot.State = contracts.StateFailed
		snapshot.Error = safeError(contracts.CancelFailed)
	case s.closed:
		snapshot.State = contracts.StateInterrupted
	case ctx.Err() != nil:
		if cleanupErr != nil {
			snapshot.State = contracts.StateFailed
			snapshot.Error = safeError(contracts.CancelFailed)
		} else {
			snapshot.State = contracts.StateCanceled
		}
	case err != nil:
		snapshot.State = contracts.StateFailed
		snapshot.Error = safeError(errorCode(err))
	case cleanupErr != nil:
		snapshot.State = contracts.StateFailed
		snapshot.Error = safeError(contracts.InternalError)
	default:
		snapshot.State = contracts.StateCompleted
		snapshot.CompletedFile = &completed
	}
	if saveErr := s.options.Store.Put(storage.Record{Fingerprint: j.fingerprint, TerminalConfirmed: terminationConfirmed, Snapshot: snapshot}); saveErr != nil {
		snapshot.State = contracts.StateFailed
		snapshot.CompletedFile = nil
		snapshot.Error = safeError(contracts.InternalError)
	}
	j.snapshot = snapshot
	if terminationConfirmed {
		s.active = nil
	}
	event := eventFrom(snapshot)
	emit := s.options.Emit
	closed := s.closed
	s.mu.Unlock()
	if terminationConfirmed {
		j.lease.Close()
	}
	if !closed && emit != nil {
		emit(event)
	}
}
func (s *Session) progress(j *job, progress contracts.Progress) {
	s.mu.Lock()
	if s.closed || s.active != j || j.snapshot.State == contracts.StateCanceling {
		s.mu.Unlock()
		return
	}
	snapshot := j.snapshot
	snapshot.Sequence++
	snapshot.State = progress.Stage
	copy := progress
	snapshot.Progress = &copy
	event := eventFrom(snapshot)
	encoded, _ := json.Marshal(event)
	if _, err := contracts.DecodeMessage(encoded); err != nil {
		s.mu.Unlock()
		return
	}
	if err := s.options.Store.Put(storage.Record{Fingerprint: j.fingerprint, Snapshot: snapshot}); err != nil {
		j.cancel()
		s.mu.Unlock()
		return
	}
	j.snapshot = snapshot
	emit := s.options.Emit
	s.mu.Unlock()
	if emit != nil {
		emit(event)
	}
}
func (s *Session) status(req contracts.Request) contracts.Response {
	payload, _ := contracts.PayloadAs[contracts.StatusPayload](req)
	s.mu.Lock()
	defer s.mu.Unlock()
	var found *job
	switch {
	case payload.JobID != "":
		found = s.jobs[payload.JobID]
	case payload.StartRequestID != "":
		found = s.starts[payload.StartRequestID]
	default:
		found = s.active
	}
	if found == nil {
		return s.success(req, contracts.StatusResult{Snapshot: nil})
	}
	snapshot := cloneSnapshot(found.snapshot)
	return s.success(req, contracts.StatusResult{Snapshot: &snapshot})
}
func (s *Session) cancelJob(req contracts.Request) contracts.Response {
	payload, _ := contracts.PayloadAs[contracts.CancelPayload](req)
	s.mu.Lock()
	j := s.jobs[payload.JobID]
	if j == nil {
		s.mu.Unlock()
		return s.failure(req, contracts.UnknownJob)
	}
	var emit func(contracts.JobEvent)
	var event contracts.JobEvent
	var saveErr error
	if s.active == j && j.snapshot.State != contracts.StateCanceling && !terminalState(j.snapshot.State) {
		j.snapshot.Sequence++
		j.snapshot.State = contracts.StateCanceling
		j.snapshot.Progress = nil
		saveErr = s.options.Store.Put(storage.Record{Fingerprint: j.fingerprint, Snapshot: j.snapshot})
		event = eventFrom(j.snapshot)
		if saveErr == nil {
			emit = s.options.Emit
		}
		j.cancel()
	}
	snapshot := cloneSnapshot(j.snapshot)
	s.mu.Unlock()
	if emit != nil {
		emit(event)
	}
	if saveErr != nil {
		return s.failure(req, contracts.InternalError)
	}
	return s.success(req, contracts.CancelResult{Snapshot: snapshot})
}
func (s *Session) reconcile(req contracts.Request) contracts.Response {
	payload, _ := contracts.PayloadAs[contracts.ReconcilePayload](req)
	if payload.PreviousHostSessionID == s.ID() {
		return s.failure(req, contracts.SessionEnded)
	}
	record, err := s.options.Store.Find(payload.PreviousHostSessionID, payload.JobID, payload.StartRequestID)
	if err != nil {
		return s.failure(req, contracts.InternalError)
	}
	result := contracts.ReconcileResult{PreviousHostSessionID: payload.PreviousHostSessionID, Outcome: "notFound", Snapshot: nil}
	if record == nil {
		return s.success(req, result)
	}
	snapshot := cloneSnapshot(record.Snapshot)
	confirmed := record.TerminalConfirmed
	if snapshot.State == contracts.StateCompleted {
		confirmed = confirmed && snapshot.CompletedFile != nil && storage.VerifyCompleted(*snapshot.CompletedFile) == nil
	}
	if confirmed {
		result.Outcome = "confirmed"
		result.Snapshot = &snapshot
	} else {
		snapshot.State = contracts.StateInterrupted
		snapshot.Progress = nil
		snapshot.CompletedFile = nil
		snapshot.Error = nil
		result.Outcome = "unconfirmed"
		result.Snapshot = &snapshot
	}
	return s.success(req, result)
}
func cloneSnapshot(snapshot contracts.JobSnapshot) contracts.JobSnapshot {
	b, _ := json.Marshal(snapshot)
	var copy contracts.JobSnapshot
	json.Unmarshal(b, &copy)
	return copy
}
func eventFrom(snapshot contracts.JobSnapshot) contracts.JobEvent {
	return contracts.JobEvent{ProtocolVersion: 1, Kind: "event", HostSessionID: snapshot.HostSessionID, JobID: snapshot.JobID, Sequence: snapshot.Sequence, State: snapshot.State, Progress: snapshot.Progress, CompletedFile: snapshot.CompletedFile, Error: snapshot.Error}
}
func terminalState(state contracts.JobState) bool {
	return state == contracts.StateCompleted || state == contracts.StateCanceled || state == contracts.StateFailed || state == contracts.StateInterrupted
}
func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	var leases []*hostwin.Lease
	for _, j := range s.jobs {
		leases = append(leases, j.lease)
	}
	s.mu.Unlock()
	for _, lease := range leases {
		lease.Close()
	}
}
func (s *Session) SetEmitter(emit func(contracts.JobEvent)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("host session has ended")
	}
	s.options.Emit = emit
	return nil
}
