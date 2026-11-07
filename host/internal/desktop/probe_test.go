package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dwnl/host/internal/contracts"
)

// restart closes s and starts the same state again with a new engine that
// reports installed as its ProbeIdentity.
func restart(t *testing.T, s *Service, c Config, installed string) (*Service, *fakeEngine) {
	t.Helper()
	s.Close()
	f := newFake()
	f.installed = installed
	c.Engine = f
	return startService(t, c), f
}

func probesOf(t *testing.T, f *fakeEngine, wantProbes, wantRestores int) {
	t.Helper()
	if probes, restores := f.probeCounts(); probes != wantProbes || restores != wantRestores {
		t.Fatalf("probes %d restores %d, want %d and %d", probes, restores, wantProbes, wantRestores)
	}
}

// Regression: every start ran the full dependency check, which launches the
// packed yt-dlp and kept the extension on "preparando ferramentas" for ~8 s.
func TestRepeatStartReusesSavedDependencyCheck(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	probesOf(t, f, 1, 0)
	s2, f2 := restart(t, s, c, fixtureInstall)
	probesOf(t, f2, 0, 1)
	if snap := snapshot(t, s2); snap.Readiness != "ready" || len(snap.Dependencies) != 4 {
		t.Fatalf("reused check: %+v", snap)
	}
}

func TestChangedInstallationIsCheckedAgain(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	_, f2 := restart(t, s, c, strings.Repeat("cd", 32))
	probesOf(t, f2, 1, 0)
}

func TestUnknownInstallationIsCheckedEveryStart(t *testing.T) {
	f := newFake()
	f.installedErr = errors.New("ffprobe.exe is missing")
	c := fixtureConfig(t, f)
	s := startService(t, c)
	if _, err := os.Stat(filepath.Join(c.StateRoot, probeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check saved without an identity: %v", err)
	}
	s.Close()
	f2 := newFake()
	f2.installedErr = f.installedErr
	c.Engine = f2
	startService(t, c)
	probesOf(t, f2, 1, 0)
}

func TestIncompleteCheckIsNeverSaved(t *testing.T) {
	f := newFake()
	f.probeErr = &contracts.ProtocolError{Code: contracts.DependencyUnavailable, Message: "A private media dependency could not be executed."}
	c := fixtureConfig(t, f)
	s := startService(t, c)
	if snap := snapshot(t, s); snap.Readiness != "degraded" {
		t.Fatalf("failed check: %+v", snap)
	}
	_, f2 := restart(t, s, c, fixtureInstall)
	probesOf(t, f2, 1, 0)
}

func TestUnusableSavedCheckIsCheckedAgain(t *testing.T) {
	saved := func(t *testing.T, c Config) savedProbe {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(c.StateRoot, probeFile))
		if err != nil {
			t.Fatal(err)
		}
		var p savedProbe
		if err = json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	encode := func(v any) []byte {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for name, damage := range map[string]func(savedProbe) []byte{
		"not JSON": func(savedProbe) []byte { return []byte(`{"version":1,`) },
		"unknown field": func(p savedProbe) []byte {
			return encode(map[string]any{"version": 1, "app": p.App, "owner": p.Owner, "identity": p.Identity, "capabilities": p.Capabilities, "dependencies": p.Dependencies, "extra": true})
		},
		"other version":   func(p savedProbe) []byte { p.Version = 2; return encode(p) },
		"older app":       func(p savedProbe) []byte { p.App = "0.2.0"; return encode(p) },
		"other owner":     func(p savedProbe) []byte { p.Owner = "S-1-5-21-0-0-0-1000"; return encode(p) },
		"missing ffprobe": func(p savedProbe) []byte { p.Dependencies = p.Dependencies[:3]; return encode(p) },
		"unavailable":     func(p savedProbe) []byte { p.Dependencies[1].Available = false; return encode(p) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			c := fixtureConfig(t, f)
			s := startService(t, c)
			if err := os.WriteFile(filepath.Join(c.StateRoot, probeFile), damage(saved(t, c)), 0600); err != nil {
				t.Fatal(err)
			}
			s2, f2 := restart(t, s, c, fixtureInstall)
			probesOf(t, f2, 1, 0)
			if snapshot(t, s2).Readiness != "ready" {
				t.Fatal("unusable saved check blocked the start")
			}
			// The fresh check replaces the damaged file.
			_, f3 := restart(t, s2, c, fixtureInstall)
			probesOf(t, f3, 0, 1)
		})
	}
}

func TestDependencyFailureInAnalysisForgetsSavedCheck(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	path := filepath.Join(c.StateRoot, probeFile)
	f.analyzeErr = &contracts.ProtocolError{Code: contracts.EngineError, Message: "The media dependency could not process this input."}
	if _, err := s.Dispatch(context.Background(), "analyze", json.RawMessage(`{"url":"https://www.youtube.com/shorts/owned"}`)); err == nil {
		t.Fatal("analysis succeeded")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("an unreadable page dropped the saved check: %v", err)
	}
	f.analyzeErr = &contracts.ProtocolError{Code: contracts.DependencyUnavailable, Message: "A private media dependency could not be executed."}
	if _, err := s.Dispatch(context.Background(), "analyze", json.RawMessage(`{"url":"https://www.youtube.com/shorts/owned"}`)); err == nil {
		t.Fatal("analysis succeeded")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("saved check survived a dependency that could not start: %v", err)
	}
	_, f2 := restart(t, s, c, fixtureInstall)
	probesOf(t, f2, 1, 0)
}

func TestDependencyFailureInDownloadForgetsSavedCheck(t *testing.T) {
	f := newFake()
	f.executeErr = &contracts.ProtocolError{Code: contracts.DependencyUnavailable, Message: "A private media dependency could not be executed."}
	c := fixtureConfig(t, f)
	s := startService(t, c)
	task := enqueue(t, s, selection(t, s, "missing-ffmpeg"))
	waitStarted(t, f)
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, task.ID) == "failed" })
	if _, err := os.Stat(filepath.Join(c.StateRoot, probeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("saved check survived a dependency that could not start: %v", err)
	}
}

// A link sent while the app prepares is searched at once; only queue changes
// wait for recovery and the dependency check.
func TestAnalysisDoesNotWaitForDependencyCheck(t *testing.T) {
	f := newFake()
	f.probeGate = make(chan struct{})
	c := fixtureConfig(t, f)
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.Dispatch(context.Background(), "analyze", json.RawMessage(`{"url":"https://www.youtube.com/shorts/owned"}`))
	if err != nil {
		t.Fatalf("analysis waited for the dependency check: %v", err)
	}
	if snap := snapshot(t, s); snap.Readiness != "preparing" {
		t.Fatalf("check finished before the test released it: %+v", snap)
	}
	a := v.(contracts.Analysis)
	p := enqueuePayload{RequestID: "early", StartPayload: contracts.StartPayload{AnalysisID: a.AnalysisID, ItemID: a.Items[0].ItemID, Mode: contracts.ModeVideo, VideoOptionID: a.Items[0].VideoChoices[0].OptionID, DestinationID: snapshot(t, s).Destination.DestinationID}}
	b, _ := json.Marshal(p)
	if _, err = s.Dispatch(context.Background(), "enqueue", b); err == nil {
		t.Fatal("queue accepted a download before recovery finished")
	}
	close(f.probeGate)
	waitFor(t, func() bool { return snapshot(t, s).Readiness == "ready" })
	enqueue(t, s, p)
}
