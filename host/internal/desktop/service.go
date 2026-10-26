// Package desktop owns the persistent queue independently of all UI clients.
package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	"dwnl/host/internal/ports"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	hostwin "dwnl/host/internal/windows"
)

const Version = "0.2.1"
const maxTasks = 200
const folderWarning = "A pasta salva não pôde ser validada; usando a pasta padrão."

type MediaEngine interface {
	ports.Engine
	Probe(context.Context) error
	Capabilities() contracts.Capabilities
	Dependencies() []contracts.Dependency
	SelectionIdentity(analysisID, itemID, optionID string) (string, error)
}

type Config struct {
	StateRoot, BinDir, DefaultDestination string
	// Engine and FolderPicker let integration tests exercise actual persistence
	// without network access or interactive Windows dialogs.
	Engine       MediaEngine
	FolderPicker storage.FolderPicker
	ProbeTimeout time.Duration
}

type Task struct {
	ID            string                   `json:"id"`
	RequestID     string                   `json:"requestId"`
	URL           string                   `json:"url"`
	Title         string                   `json:"title"`
	Mode          contracts.DownloadMode   `json:"mode"`
	BitrateKbps   int                      `json:"bitrateKbps,omitempty"`
	State         string                   `json:"state"`
	CreatedAt     string                   `json:"createdAt"`
	Error         string                   `json:"error,omitempty"`
	Progress      *contracts.Progress      `json:"progress,omitempty"`
	CompletedFile *contracts.CompletedFile `json:"completedFile,omitempty"`
}

type Snapshot struct {
	Version      string                 `json:"version"`
	Readiness    string                 `json:"readiness"`
	Capabilities contracts.Capabilities `json:"capabilities"`
	Dependencies []contracts.Dependency `json:"dependencies"`
	Destination  contracts.Destination  `json:"destination"`
	Paused       bool                   `json:"paused"`
	Tasks        []Task                 `json:"tasks"`
	Error        string                 `json:"error,omitempty"`
	// Legacy records remain in the original ledger. They cannot be safely
	// reconstructed as queued work because the old format did not save sources.
	LegacyHistory []contracts.JobSnapshot `json:"legacyHistory,omitempty"`
}

type enqueuePayload struct {
	RequestID string `json:"requestId"`
	contracts.StartPayload
}

type queuedRecord struct {
	Task            Task                   `json:"task"`
	Selection       contracts.StartPayload `json:"selection"`
	Identity        string                 `json:"identity"`
	Fingerprint     string                 `json:"fingerprint"`
	DestinationPath string                 `json:"destinationPath"`
	AttemptSession  string                 `json:"attemptSession,omitempty"`
	AttemptJob      string                 `json:"attemptJob,omitempty"`
}

type cursor struct {
	analysis, item string
	offset         int
}

type Service struct {
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	closeOnce          sync.Once
	closed             bool
	engine             MediaEngine
	store              *storage.Store
	destinations       *storage.Destinations
	sessionID          string
	lease              *hostwin.Lease
	retainedLeases     []*hostwin.Lease
	tasks              []queuedRecord
	legacy             []contracts.JobSnapshot
	analyses           map[string]contracts.Analysis
	analysisOrder      []string
	cursors            map[string]cursor
	analyzeSlot        chan struct{}
	wake               chan struct{}
	readiness, warning string
	capabilities       contracts.Capabilities
	dependencies       []contracts.Dependency
	paused, blocked    bool
	activeID           string
	activeCancel       context.CancelFunc
}

func New(config Config) (*Service, error) {
	root, err := filepath.Abs(config.StateRoot)
	if err != nil || config.StateRoot == "" {
		return nil, errors.New("a private state root is required")
	}
	lease, busy, err := hostwin.AcquireLease("desktop-queue:"+strings.ToLower(root), false)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errors.New("the desktop queue is already open")
	}
	success := false
	defer func() {
		if !success {
			lease.Close()
		}
	}()
	store, err := storage.Open(root)
	if err != nil {
		return nil, fmt.Errorf("open desktop state: %w", err)
	}
	if err = store.InitializeOwnership(); err != nil {
		return nil, err
	}
	preferred, preferenceErr := store.PreferredDestination(config.DefaultDestination)
	if preferred == "" {
		return nil, preferenceErr
	}
	destinations, err := storage.NewDestinations(preferred, config.FolderPicker)
	if err != nil {
		return nil, err
	}
	sessionID := hostruntime.NewID()
	media := config.Engine
	if media == nil {
		runner, err := hostwin.NewRunner(config.BinDir)
		if err != nil {
			return nil, err
		}
		media, err = engine.New(engine.Config{HostSessionID: sessionID, Runner: runner, PrivateBinDir: config.BinDir, AnalysisRoot: filepath.Join(root, "analyses")})
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{ctx: ctx, cancel: cancel, engine: media, store: store, destinations: destinations, sessionID: sessionID, lease: lease,
		tasks: []queuedRecord{}, analyses: map[string]contracts.Analysis{}, cursors: map[string]cursor{}, analyzeSlot: make(chan struct{}, 1), wake: make(chan struct{}, 1), readiness: "preparing", dependencies: []contracts.Dependency{}}
	if preferenceErr != nil {
		s.warning = folderWarning
	}
	if err = s.load(); err != nil {
		cancel()
		return nil, err
	}
	if err = s.persist(); err != nil {
		cancel()
		return nil, err
	}
	if config.ProbeTimeout <= 0 {
		config.ProbeTimeout = 20 * time.Second
	}
	s.wg.Add(2)
	go s.probe(config.ProbeTimeout)
	go s.worker()
	success = true
	return s, nil
}

func (s *Service) probe(timeout time.Duration) {
	defer s.wg.Done()
	// Only snapshot is served during recovery; hashes and file identity checks
	// run outside the mutex so an already running UI can connect immediately.
	recoveryErr := s.recover()
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	err := s.engine.Probe(ctx)
	s.mu.Lock()
	if recoveryErr != nil {
		s.persistFailure(recoveryErr)
	}
	if saveErr := s.persist(); saveErr != nil {
		s.persistFailure(saveErr)
	}
	s.capabilities = s.engine.Capabilities()
	s.dependencies = s.engine.Dependencies()
	if s.dependencies == nil {
		s.dependencies = []contracts.Dependency{}
	}
	if err != nil {
		s.readiness = "degraded"
		s.warning = strings.TrimSpace(s.warning + " Preparação incompleta: " + safeError(err))
	} else {
		s.readiness = "ready"
		if s.blocked {
			s.readiness = "degraded"
		}
	}
	s.mu.Unlock()
	s.signal()
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		s.mu.Unlock()
		s.wg.Wait()
		for _, lease := range s.retainedLeases {
			lease.Close()
		}
		s.lease.Close()
	})
}

func (s *Service) snapshotLocked() Snapshot {
	out := Snapshot{Version: Version, Readiness: s.readiness, Capabilities: s.capabilities, Dependencies: append([]contracts.Dependency{}, s.dependencies...), Destination: s.destinations.Default(), Paused: s.paused, Tasks: []Task{}, Error: s.warning, LegacyHistory: s.legacy}
	for _, record := range s.tasks {
		out.Tasks = append(out.Tasks, cloneTask(record.Task))
	}
	return out
}

// cloneTask deep-copies pointers so snapshots never alias queue state.
func cloneTask(t Task) Task {
	if t.Progress != nil {
		p := *t.Progress
		p.CompletedBytes, p.TotalBytes = clonePtr(p.CompletedBytes), clonePtr(p.TotalBytes)
		p.ProcessedSeconds, p.DurationSeconds, p.Percent = clonePtr(p.ProcessedSeconds), clonePtr(p.DurationSeconds), clonePtr(p.Percent)
		t.Progress = &p
	}
	t.CompletedFile = clonePtr(t.CompletedFile)
	return t
}
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Dispatch owns no task lifecycle: canceling the caller only cancels that
// caller's analysis or picker, never already accepted queue entries.
func (s *Service) Dispatch(ctx context.Context, method string, payload json.RawMessage) (any, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("O aplicativo está encerrando.")
	}
	// Track all requests under the same lock as Close before adding workers.
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	switch method {
	case "snapshot":
		var p struct{}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.snapshotLocked(), nil
	case "analyze":
		req, err := s.request(contracts.OperationAnalyze, "analyze", payload)
		if err != nil {
			return nil, err
		}
		p, _ := contracts.PayloadAs[contracts.AnalyzePayload](req)
		return s.analyze(work, p.URL)
	case "listFormats":
		req, err := s.request(contracts.OperationListFormats, "formats", payload)
		if err != nil {
			return nil, err
		}
		return s.listFormats(req)
	case "enqueue":
		var p enqueuePayload
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		return s.enqueue(p)
	case "cancel":
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		return s.cancelTask(p.ID)
	case "continue":
		var p struct{}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		return s.continueQueue()
	case "archiveHistory":
		var p struct{}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		return s.archiveHistory(work)
	case "chooseDestination":
		var p struct{}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		chosen, err := s.destinations.Choose(work)
		if err != nil {
			return nil, err
		}
		if !chosen.Canceled {
			if err = s.store.SaveDestinationPreference(chosen.Label); err != nil {
				return nil, fmt.Errorf("A pasta foi selecionada, mas não foi possível salvar a preferência: %w", err)
			}
			s.mu.Lock()
			if s.warning == folderWarning {
				s.warning = ""
			}
			s.mu.Unlock()
		}
		return chosen, nil
	default:
		return nil, errors.New("Comando do aplicativo desconhecido.")
	}
}

func (s *Service) request(op contracts.Operation, id string, payload json.RawMessage) (contracts.Request, error) {
	b, err := json.Marshal(contracts.Request{ProtocolVersion: 1, RequestID: id, HostSessionID: s.sessionID, Operation: op, Payload: payload})
	if err != nil {
		return contracts.Request{}, err
	}
	return contracts.DecodeRequest(b)
}
func safeError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "O tempo limite foi atingido. Tente novamente."
	}
	if errors.Is(err, context.Canceled) {
		return "Operação interrompida."
	}
	var p *contracts.ProtocolError
	if errors.As(err, &p) {
		return string(p.Code) + ": " + p.Message
	}
	var d *contracts.ProtocolDecodeError
	if errors.As(err, &d) {
		return string(d.Code) + ": " + d.Message
	}
	return "Falha local ao executar ou salvar a operação. Verifique a pasta e o espaço disponível."
}
func fingerprint(p contracts.StartPayload) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
