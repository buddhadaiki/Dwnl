package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	hostwin "dwnl/host/internal/windows"
)

// probeFile keeps the last complete dependency check. Checking runs yt-dlp,
// which takes seconds to start, so a start whose installed dependencies are
// unchanged adopts the saved result instead.
const probeFile = "probe-cache.json"
const maxProbeBytes = 64 * 1024

var errUnusableProbe = errors.New("saved dependency check is unusable")

type savedProbe struct {
	Version int `json:"version"`
	// App is the Dwnl version that saved the check. A newer version may check
	// more, so it never adopts an older version's result.
	App          string                 `json:"app"`
	Owner        string                 `json:"owner"`
	Identity     string                 `json:"identity"`
	Capabilities contracts.Capabilities `json:"capabilities"`
	Dependencies []contracts.Dependency `json:"dependencies"`
}

// probe settles readiness. Only snapshot is served during recovery; hashes and
// file identity checks run outside the mutex so an already running UI can
// connect immediately.
func (s *Service) probe(timeout time.Duration) {
	defer s.wg.Done()
	recoveryErr := s.recover()
	identity, identityErr := s.engine.ProbeIdentity()
	var err error
	if identityErr != nil || !s.restoreProbe(identity) {
		ctx, cancel := context.WithTimeout(s.ctx, timeout)
		err = s.engine.Probe(ctx)
		cancel()
		// A dependency replaced while it was being checked must not inherit
		// the result, so the identity is taken again before saving.
		if err == nil && identityErr == nil {
			if after, e := s.engine.ProbeIdentity(); e == nil && after == identity {
				s.saveProbe(identity)
			}
		}
	}
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

// restoreProbe adopts the saved check when it was made for this identity.
func (s *Service) restoreProbe(identity string) bool {
	saved, err := s.readProbe()
	if err != nil || saved.Identity != identity {
		return false
	}
	s.engine.RestoreProbe(saved.Capabilities, saved.Dependencies)
	return true
}

func (s *Service) readProbe() (savedProbe, error) {
	path := filepath.Join(s.store.Root(), probeFile)
	unlock, err := hostwin.LockPath(path, false)
	if err != nil {
		return savedProbe{}, err
	}
	defer unlock()
	f, err := os.Open(path)
	if err != nil {
		return savedProbe{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProbeBytes+1))
	if err != nil {
		return savedProbe{}, err
	}
	owner, err := hostwin.UserID()
	if err != nil {
		return savedProbe{}, err
	}
	var saved savedProbe
	if len(data) > maxProbeBytes || decode(data, &saved) != nil || saved.Version != 1 || saved.App != Version || saved.Owner != owner || !digestPattern.MatchString(saved.Identity) || !completeDependencies(saved.Dependencies) {
		return savedProbe{}, errUnusableProbe
	}
	return saved, nil
}

// Only a check that found every dependency is ever saved.
func completeDependencies(dependencies []contracts.Dependency) bool {
	roles := []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe}
	if len(dependencies) != len(roles) {
		return false
	}
	for i, d := range dependencies {
		if d.Name != string(roles[i]) || !d.Available || d.Version == "" || len(d.Version) > 512 {
			return false
		}
	}
	return true
}

// saveProbe is best effort: without a saved check the next start only checks
// every dependency again.
func (s *Service) saveProbe(identity string) {
	owner, err := hostwin.UserID()
	if err != nil {
		return
	}
	saved := savedProbe{Version: 1, App: Version, Owner: owner, Identity: identity, Capabilities: s.engine.Capabilities(), Dependencies: s.engine.Dependencies()}
	if !completeDependencies(saved.Dependencies) {
		return
	}
	data, err := json.Marshal(saved)
	if err == nil {
		_ = s.replaceStateFile(probeFile, data)
	}
}

// forgetProbe drops the saved check after a private executable failed to
// start, so the next start checks every dependency again instead of trusting
// a result the installation no longer matches.
func (s *Service) forgetProbe(err error) {
	if code, ok := protocolCode(err); !ok || code != contracts.DependencyUnavailable {
		return
	}
	root := s.store.Root()
	unlock, lockErr := hostwin.LockPath(root, false)
	if lockErr != nil {
		return
	}
	defer unlock()
	path := filepath.Join(root, probeFile)
	if hostwin.CheckPath(path, true) == nil {
		_ = os.Remove(path)
	}
}
