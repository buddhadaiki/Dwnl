// Package engine owns extraction policy and local media processing.
package engine

import (
	"context"
	"crypto/rand"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	HostSessionID string
	Runner        ports.ProcessRunner
}
type Engine struct {
	config       Config
	mu           sync.Mutex
	capabilities contracts.Capabilities
	dependencies []contracts.Dependency
	analyses     map[string]localAnalysis
}

var _ ports.Engine = (*Engine)(nil)

type localAnalysis struct {
	analysis contracts.Analysis
	media    LocalMedia
	probe    mediaProbe
	audio    mediaProbe
	proofs   []fileProof
}
type LocalMedia struct {
	VideoPath string
	AudioPath string
	Title     string
}

func New(config Config) (*Engine, error) {
	if config.HostSessionID == "" || config.Runner == nil {
		return nil, errors.New("engine requires a host session and private process runner")
	}
	return &Engine{config: config, analyses: make(map[string]localAnalysis)}, nil
}
func protocolError(c contracts.ErrorCode, message string) error {
	return &contracts.ProtocolError{Code: c, Message: message}
}
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("operating system random source unavailable")
	}
	return hex.EncodeToString(b)
}
func cloneAnalysis(a contracts.Analysis) contracts.Analysis {
	b, _ := json.Marshal(a)
	var out contracts.Analysis
	_ = json.Unmarshal(b, &out)
	return out
}
func (e *Engine) Capabilities() contracts.Capabilities {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.capabilities
}
func (e *Engine) Dependencies() []contracts.Dependency {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]contracts.Dependency(nil), e.dependencies...)
}
func (e *Engine) Analyze(ctx context.Context, raw string) (contracts.Analysis, error) {
	if err := ctx.Err(); err != nil {
		return contracts.Analysis{}, err
	}
	if _, err := CanonicalSourceURL(raw); err != nil {
		return contracts.Analysis{}, err
	}
	// ProcessRunner is a subprocess boundary, not a per-request transport. Do not
	// start yt-dlp until every extractor/CDN/redirect connection can be constrained.
	return contracts.Analysis{}, protocolError(contracts.NetworkPolicyBlocked, "Platform access is unavailable because the extractor network policy cannot be enforced.")
}
func (e *Engine) run(ctx context.Context, spec ports.ExecSpec, line func(ports.ProcessLine)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateExecSpec(spec); err != nil {
		return err
	}
	var noSpace atomic.Bool
	result, err := e.config.Runner.Run(ctx, spec, func(value ports.ProcessLine) {
		if value.Stream == ports.Stderr {
			text := strings.ToLower(value.Text)
			if strings.Contains(text, "no space left on device") || strings.Contains(text, "disk is full") {
				noSpace.Store(true)
			}
		}
		if line != nil {
			line(value)
		}
	})
	var explicit *contracts.ProtocolError
	if errors.As(err, &explicit) {
		return explicit
	}
	if ctx.Err() != nil || result.Canceled {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return context.Canceled
	}
	if result.ExitCode != 0 {
		if noSpace.Load() {
			return protocolError(contracts.DiskFull, "The media disk has no space left.")
		}
		return protocolError(contracts.EngineError, "The media dependency could not process this input.")
	}
	if err != nil {
		if diskFull(err) {
			return protocolError(contracts.DiskFull, "The media disk has no space left.")
		}
		return protocolError(contracts.DependencyUnavailable, "A private media dependency could not be executed.")
	}
	return nil
}
func (e *Engine) capture(ctx context.Context, spec ports.ExecSpec) (string, error) {
	var output []byte
	overflow := false
	err := e.run(ctx, spec, func(line ports.ProcessLine) {
		if line.Stream != ports.Stdout {
			return
		}
		if len(output)+len(line.Text)+1 > 2*1024*1024 {
			overflow = true
			return
		}
		output = append(output, []byte(line.Text)...)
		output = append(output, '\n')
	})
	if err != nil {
		return "", err
	}
	if overflow {
		return "", protocolError(contracts.EngineError, "Media metadata exceeded the supported limit.")
	}
	return string(output), nil
}
func (e *Engine) remember(a localAnalysis) contracts.Analysis {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, old := range e.analyses {
		expires, _ := time.Parse(time.RFC3339, old.analysis.ExpiresAt)
		if !time.Now().Before(expires) {
			delete(e.analyses, id)
		}
	}
	// The host allows one job and caches analyses briefly. Keep memory bounded.
	if len(e.analyses) >= 20 {
		for id := range e.analyses {
			delete(e.analyses, id)
			break
		}
	}
	e.analyses[a.analysis.AnalysisID] = a
	return cloneAnalysis(a.analysis)
}
