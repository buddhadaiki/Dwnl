package native

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dwnl/host/internal/ports"
	"dwnl/host/internal/testutil"
	hostwin "dwnl/host/internal/windows"
	win "golang.org/x/sys/windows"
)

func buildChromeFixture(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Only the role name is reused: this private executable is our test fixture.
	fixture := filepath.Join(testutil.TempDir(t), "deno.exe")
	build := exec.Command(filepath.Join(repo, ".tools", "go", "bin", "go.exe"), "-C", filepath.Join(repo, "host"), "build", "-o", fixture, "./internal/native/testdata/chromepipefixture")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v %s", err, output)
	}
	return fixture
}

type chromeFixtureProcess struct {
	done   chan struct{}
	result ports.ProcessResult
	err    error
	mu     sync.Mutex
	lines  []string
	child  win.Handle
}

func startChromeFixture(t *testing.T, ctx context.Context, cancel context.CancelFunc, fixture, launcher, mode string, input, output *chromeServerPipe) *chromeFixtureProcess {
	t.Helper()
	runner, err := hostwin.NewRunner(filepath.Dir(fixture))
	if err != nil {
		t.Fatal(err)
	}
	root := testutil.TempDir(t)
	env := []string{
		"DWNL_PIPE_FIXTURE_MODE=launcher", "DWNL_PIPE_FIXTURE_CHILD_MODE=" + mode,
		"DWNL_PIPE_FIXTURE_LAUNCHER=" + launcher, "DWNL_PIPE_FIXTURE_ROOT=" + root,
		"DWNL_PIPE_FIXTURE_INPUT=" + input.name, "DWNL_PIPE_FIXTURE_OUTPUT=" + output.name,
		"TEMP=" + root, "TMP=" + root,
	}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	p := &chromeFixtureProcess{done: make(chan struct{})}
	go func() {
		// The real runner atomically associates the launcher with a kill-on-close
		// Job Object. CMD and fixture children inherit that exact job; cancellation
		// confirms its entire tree has exited and both captured streams drained.
		p.result, p.err = runner.Run(ctx, ports.ExecSpec{Executable: ports.Deno, Args: []string{"chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/", "--parent-window=0"}, Dir: filepath.Dir(fixture), Env: env}, func(line ports.ProcessLine) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.lines = append(p.lines, line.Text)
			var pid uint32
			if _, err := fmt.Sscanf(line.Text, "stage=stall pid=%d", &pid); err == nil && p.child == 0 {
				// Retain the exact live fixture handle for a physical exit check, not
				// process discovery or termination by PID/image name.
				h, err := win.OpenProcess(win.SYNCHRONIZE|win.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
				if err == nil {
					var name [32768]uint16
					n := uint32(len(name))
					if win.QueryFullProcessImageName(h, 0, &name[0], &n) == nil && strings.EqualFold(win.UTF16ToString(name[:n]), fixture) {
						p.child = h
					} else {
						win.CloseHandle(h)
					}
				}
			}
		})
		close(p.done)
	}()
	t.Cleanup(func() {
		cancel()
		if !p.wait(12 * time.Second) {
			t.Error("owned fixture runner did not finish within cancellation/drain budget")
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.child != 0 {
			win.CloseHandle(p.child)
			p.child = 0
		}
	})
	return p
}

func (p *chromeFixtureProcess) wait(timeout time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (p *chromeFixtureProcess) stderr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.lines, "\n")
}

func (p *chromeFixtureProcess) childExited() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.child == 0 {
		return false, fmt.Errorf("stalled fixture identity was not retained")
	}
	status, err := win.WaitForSingleObject(p.child, 0)
	return status == win.WAIT_OBJECT_0, err
}
