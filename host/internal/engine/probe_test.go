package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/testutil"
)

// probeRunner answers the probe's commands like the private executables do.
// The adapter self-test waits until every quick check has run, so a probe that
// runs them only after yt-dlp fails instead of passing slowly.
type probeRunner struct {
	mu      sync.Mutex
	starts  []ports.ExecutableRole
	ready   string
	exit    int
	missing map[ports.ExecutableRole]bool
	quick   chan struct{}
}

func newProbeRunner() *probeRunner {
	return &probeRunner{ready: "[Dwnl] DWNL_ADAPTER_READY 2026.08.19", missing: map[ports.ExecutableRole]bool{}, quick: make(chan struct{}, 8)}
}

func (r *probeRunner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	r.mu.Lock()
	r.starts = append(r.starts, s.Executable)
	r.mu.Unlock()
	if r.missing[s.Executable] {
		// The runners report -1 for a process that never started.
		return ports.ProcessResult{ExitCode: -1}, errors.New("The system cannot find the file specified.")
	}
	out := func(text string) { emit(ports.ProcessLine{Stream: ports.Stdout, Text: text}) }
	switch {
	case s.Executable == ports.YTDLP && slices.Contains(s.Args, "dwnl:mode=probe"):
		for range 4 {
			select {
			case <-r.quick:
			case <-time.After(5 * time.Second):
				return ports.ProcessResult{ExitCode: 1}, nil
			}
		}
		out(r.ready)
		return ports.ProcessResult{ExitCode: r.exit}, nil
	case s.Executable == ports.YTDLP:
		out("2026.08.19")
	case s.Executable == ports.Deno:
		out("deno 2.9.7 (stable, release, x86_64-pc-windows-msvc)")
	case slices.Contains(s.Args, "-encoders"):
		out(" A....D libmp3lame           libmp3lame MP3 (MPEG audio layer 3) (codec mp3)")
	default:
		out(string(s.Executable) + " version 9.0.2-essentials_build-www.gyan.dev")
	}
	r.quick <- struct{}{}
	return ports.ProcessResult{}, nil
}

func (r *probeRunner) ytdlpStarts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, role := range r.starts {
		if role == ports.YTDLP {
			n++
		}
	}
	return n
}

func probeEngine(t *testing.T, r ports.ProcessRunner) *Engine {
	t.Helper()
	root := testutil.TempDir(t)
	e, err := New(Config{HostSessionID: "session", Runner: r, PrivateBinDir: filepath.Join(root, "bin"), AnalysisRoot: filepath.Join(root, "analyses")})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// Regression: the probe started the packed yt-dlp twice, once for --version
// and once for the adapter self-test, about 3.5 s each.
func TestProbeStartsYTDLPOnceAndReadsItsVersionFromTheAdapter(t *testing.T) {
	r := newProbeRunner()
	e := probeEngine(t, r)
	if err := e.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := r.ytdlpStarts(); n != 1 {
		t.Fatalf("yt-dlp started %d times", n)
	}
	if got := e.Capabilities(); got != (contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}) {
		t.Fatalf("capabilities: %+v", got)
	}
	want := []contracts.Dependency{{Name: "yt-dlp", Version: "2026.08.19", Available: true}, {Name: "deno", Version: "deno 2.9.7 (stable, release, x86_64-pc-windows-msvc)", Available: true}, {Name: "ffmpeg", Version: "ffmpeg version 9.0.2-essentials_build-www.gyan.dev", Available: true}, {Name: "ffprobe", Version: "ffprobe version 9.0.2-essentials_build-www.gyan.dev", Available: true}}
	if got := e.Dependencies(); !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies: %+v", got)
	}
}

func TestProbeFallsBackToVersionWhenTheAdapterFails(t *testing.T) {
	for name, adjust := range map[string]func(*probeRunner){
		"exit":          func(r *probeRunner) { r.exit = 1 },
		"missing ready": func(r *probeRunner) { r.ready = "[Dwnl] something else" },
	} {
		t.Run(name, func(t *testing.T) {
			r := newProbeRunner()
			adjust(r)
			e := probeEngine(t, r)
			code(t, e.Probe(context.Background()), contracts.EngineError)
			if got := e.Capabilities(); got != (contracts.Capabilities{Video: true, MP3: true}) {
				t.Fatalf("capabilities: %+v", got)
			}
			if got := e.Dependencies()[0]; got != (contracts.Dependency{Name: "yt-dlp", Version: "2026.08.19", Available: true}) {
				t.Fatalf("an installed yt-dlp with a broken adapter was reported as %+v", got)
			}
		})
	}
}

func TestProbeReportsAMissingExecutableAsUnavailable(t *testing.T) {
	r := newProbeRunner()
	r.missing[ports.YTDLP] = true
	e := probeEngine(t, r)
	code(t, e.Probe(context.Background()), contracts.DependencyUnavailable)
	if got := e.Dependencies()[0]; got != (contracts.Dependency{Name: "yt-dlp", Version: "unavailable"}) {
		t.Fatalf("missing yt-dlp reported as %+v", got)
	}
	if got := e.Capabilities(); got != (contracts.Capabilities{Video: true, MP3: true}) {
		t.Fatalf("capabilities: %+v", got)
	}
}

func TestProbeIdentityFollowsTheYTDLPRuntimeFolder(t *testing.T) {
	root := testutil.TempDir(t)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, role := range []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe} {
		if err := os.WriteFile(filepath.Join(bin, ports.ExecutableFile(role)), []byte(role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	e, err := New(Config{HostSessionID: "session", Runner: &neverRunner{}, PrivateBinDir: bin, AnalysisRoot: filepath.Join(root, "analyses")})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	step := func(name string) {
		t.Helper()
		id, err := e.ProbeIdentity()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if earlier, ok := seen[id]; ok {
			t.Fatalf("%s kept the identity of %s", name, earlier)
		}
		seen[id] = name
	}
	step("single-file build")
	runtimeDir := filepath.Join(bin, "_internal")
	library := filepath.Join(runtimeDir, "lib", "module.bin")
	if err := os.MkdirAll(filepath.Dir(library), 0700); err != nil {
		t.Fatal(err)
	}
	step("empty runtime folder")
	if err := os.WriteFile(library, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	step("runtime member added")
	if err := os.WriteFile(library, []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	step("runtime member resized")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(library, later, later); err != nil {
		t.Fatal(err)
	}
	step("runtime member rewritten in place")
	if err := os.Rename(library, filepath.Join(runtimeDir, "module.bin")); err != nil {
		t.Fatal(err)
	}
	step("runtime member moved")
	// Windows may finish deleting a just-scanned member a moment later.
	if err := testutil.RetryDirectoryNotEmpty(func() error { return os.RemoveAll(runtimeDir) }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimeDir, []byte("not a folder"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ProbeIdentity(); err == nil {
		t.Fatal("identity with a file named _internal")
	}
}

func TestProbeIdentityFollowsTheInstalledExecutables(t *testing.T) {
	root := testutil.TempDir(t)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	file := func(role ports.ExecutableRole) string { return filepath.Join(bin, ports.ExecutableFile(role)) }
	for _, role := range []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe} {
		if err := os.WriteFile(file(role), []byte(role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	e, err := New(Config{HostSessionID: "session", Runner: &neverRunner{}, PrivateBinDir: bin, AnalysisRoot: filepath.Join(root, "analyses")})
	if err != nil {
		t.Fatal(err)
	}
	identity := func() string {
		t.Helper()
		id, err := e.ProbeIdentity()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := identity()
	if len(first) != 64 || strings.Trim(first, "0123456789abcdef") != "" || identity() != first {
		t.Fatalf("identity is not a stable digest: %q", first)
	}
	if err = os.WriteFile(file(ports.YTDLP), []byte("updated yt-dlp"), 0700); err != nil {
		t.Fatal(err)
	}
	updated := identity()
	if updated == first {
		t.Fatal("an updated yt-dlp kept the identity")
	}
	replacement := filepath.Join(root, "ffmpeg.new")
	if err = os.WriteFile(replacement, []byte(ports.FFmpeg), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, file(ports.FFmpeg)); err != nil {
		t.Fatal(err)
	}
	if identity() == updated {
		t.Fatal("a replaced ffmpeg kept the identity")
	}
	if err = os.Remove(file(ports.FFprobe)); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ProbeIdentity(); err == nil {
		t.Fatal("identity without ffprobe")
	}
	if err = os.Mkdir(file(ports.FFprobe), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ProbeIdentity(); err == nil {
		t.Fatal("identity with a folder named ffprobe")
	}
	relative, _ := New(Config{HostSessionID: "session", Runner: &neverRunner{}, PrivateBinDir: "bin", AnalysisRoot: "analyses"})
	if _, err = relative.ProbeIdentity(); err == nil {
		t.Fatal("identity of relative folders")
	}
}

func TestRestoreProbeAdoptsASavedResultWithoutStartingAnything(t *testing.T) {
	r := &neverRunner{}
	e := probeEngine(t, r)
	capabilities := contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}
	saved := []contracts.Dependency{{Name: "yt-dlp", Version: "2026.08.19", Available: true}}
	e.RestoreProbe(capabilities, saved)
	saved[0].Version = "changed by the caller"
	if e.Capabilities() != capabilities || e.Dependencies()[0].Version != "2026.08.19" || r.calls != 0 {
		t.Fatalf("restore: %+v %+v %d", e.Capabilities(), e.Dependencies(), r.calls)
	}
}
