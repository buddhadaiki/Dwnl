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

const Version = "0.4.1"
const maxTasks = 200
const folderWarning = "A pasta salva não pôde ser validada; usando a pasta padrão."

type MediaEngine interface {
	ports.Engine
	Probe(context.Context) error
	Capabilities() contracts.Capabilities
	Dependencies() []contracts.Dependency
	// ProbeIdentity and RestoreProbe let a start reuse the last complete probe
	// while the installed dependencies are unchanged.
	ProbeIdentity() (string, error)
	RestoreProbe(contracts.Capabilities, []contracts.Dependency)
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
	RenamedFrom     *renameOrigin          `json:"renamedFrom,omitempty"`
}

type cursor struct {
	analysis, item string
	offset         int
}

type Service struct {
	mu                 sync.Mutex
	historyMu          sync.Mutex
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
func (s *Service) Dispatch(ctx context.Context, method string, payload json.RawMessage) (_ any, err error) {
	defer func() { err = dispatchError(method, err) }()
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
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		return s.archiveHistory(work, p.ID)
	case "renameTask":
		var p struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := decode(payload, &p); err != nil {
			return nil, err
		}
		return s.renameTask(p.ID, p.Title)
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

// Engine and contract messages are English diagnostics, so the window shows
// only these fixed texts keyed by the stable protocol code.
var protocolMessages = map[contracts.ErrorCode]string{
	contracts.InvalidMessage:        "Os dados da operação não passaram na validação.",
	contracts.MessageTooLarge:       "Os dados da operação excedem o tamanho permitido.",
	contracts.IncompatibleProtocol:  "Os componentes do Dwnl usam versões incompatíveis. Reinstale o aplicativo.",
	contracts.UnknownOperation:      "A operação não está disponível nesta versão do Dwnl.",
	contracts.InvalidSelection:      "A opção escolhida não está mais disponível. Analise o link novamente.",
	contracts.SessionEnded:          "A sessão anterior terminou. Analise o link novamente.",
	contracts.AnalysisExpired:       "A análise expirou. Analise o link novamente.",
	contracts.InvalidCursor:         "Não foi possível carregar os formatos. Analise o link novamente.",
	contracts.Busy:                  "Já existe uma tarefa ativa. Aguarde antes de iniciar outra.",
	contracts.RequestConflict:       "A identidade desta solicitação já foi usada com outra seleção.",
	contracts.UnknownJob:            "A tarefa não foi encontrada.",
	contracts.DependencyUnavailable: "Um componente de mídia não está disponível. Confira a instalação do Dwnl.",
	contracts.MP3Unavailable:        "A conversão para MP3 não está disponível nesta instalação.",
	contracts.NoAudio:               "Este conteúdo não tem áudio disponível.",
	contracts.AccessRestricted:      "A plataforma bloqueou o acesso a este conteúdo.",
	contracts.NetworkError:          "A conexão com a plataforma falhou. Tente novamente.",
	contracts.NetworkPolicyBlocked:  "O acesso à rede foi bloqueado pela proteção do Dwnl.",
	contracts.DestinationError:      "Não foi possível gravar na pasta escolhida. Escolha outra pasta.",
	contracts.DiskFull:              "O disco está cheio. Libere espaço ou escolha outra pasta.",
	contracts.EngineError:           "Um componente de mídia não conseguiu concluir a operação.",
	contracts.CancelFailed:          "O encerramento da tarefa não foi confirmado.",
	contracts.InvalidOutput:         "O arquivo não passou na verificação. A tarefa não foi concluída.",
	contracts.InternalError:         "O Dwnl encontrou um erro interno.",
}

const invalidSourceMessage = "Use um link HTTPS do YouTube, Instagram, TikTok ou X, sem usuário ou senha."
const destinationMessage = "A pasta de destino não pôde ser usada. Escolha a pasta novamente."

func safeError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "O tempo limite foi atingido. Tente novamente."
	}
	if errors.Is(err, context.Canceled) {
		return "Operação interrompida."
	}
	if code, ok := protocolCode(err); ok {
		return protocolText(code)
	}
	return "Falha local ao executar ou salvar a operação. Verifique a pasta e o espaço disponível."
}

// protocolText is the fixed Portuguese text for a protocol code; unknown codes
// read as an internal error.
func protocolText(code contracts.ErrorCode) string {
	if message, known := protocolMessages[code]; known {
		return message
	}
	return protocolMessages[contracts.InternalError]
}

// Up to 0.2.1 a failure was saved as "CODE: English diagnostic". A saved text
// that starts with a known code shows that code's Portuguese text instead.
func savedErrorText(text string) string {
	code, _, found := strings.Cut(text, ": ")
	if _, known := protocolMessages[contracts.ErrorCode(code)]; found && known {
		return protocolMessages[contracts.ErrorCode(code)]
	}
	return text
}
func protocolCode(err error) (contracts.ErrorCode, bool) {
	var p *contracts.ProtocolError
	if errors.As(err, &p) {
		return p.Code, true
	}
	var d *contracts.ProtocolDecodeError
	if errors.As(err, &d) {
		return d.Code, true
	}
	return "", false
}

// displayError replaces only the text: the wrapped error stays reachable, so
// the extension bridge still reports the protocol code it branches on.
type displayError struct {
	message string
	err     error
}

func (e *displayError) Error() string { return e.message }
func (e *displayError) Unwrap() error { return e.err }

// dispatchError leaves this package's own Portuguese errors untouched and
// rewrites engine, contract and context errors, whose text is English.
func dispatchError(method string, err error) error {
	code, typed := protocolCode(err)
	if err == nil || !typed && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	message := safeError(err)
	if method == "analyze" && code == contracts.InvalidMessage {
		message = invalidSourceMessage
	}
	return &displayError{message: message, err: err}
}
func fingerprint(p contracts.StartPayload) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
