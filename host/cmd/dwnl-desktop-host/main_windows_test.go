package main

import (
	"bytes"
	"context"
	"dwnl/host/internal/desktopipc"
	"dwnl/host/internal/native"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("DWNL_BRIDGE_TEST_HELPER") == "1" {
		err := serveBridge(os.Getenv("DWNL_BRIDGE_TEST_STATE"), "not-installed.exe", os.Stdin, os.Stdout, func(string) error { return errors.New("unexpected launch in helper") })
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestProductionRejectsForeignOriginBeforeResolvingPaths(t *testing.T) {
	previous := os.Args
	defer func() { os.Args = previous }()
	for _, args := range [][]string{{"host"}, {"host", "https://example.com"}, {"host", origin + "popup.html"}} {
		os.Args = args
		if err := run(); err == nil || err.Error() != "origem não autorizada" {
			t.Fatalf("origin accepted: %q: %v", args, err)
		}
	}
}

func TestConnectorCanRestartAndConcurrentCallersShareEachLaunch(t *testing.T) {
	state := t.TempDir()
	var server *desktopipc.Server
	var launches atomic.Int32
	connector := appConnector{state: state, app: "fixture.exe", launch: func(app string) error {
		if app != "fixture.exe" {
			return errors.New("wrong app")
		}
		launches.Add(1)
		time.Sleep(10 * time.Millisecond)
		var err error
		server, err = desktopipc.Listen(state, func(context.Context, string, json.RawMessage) (any, error) {
			return map[string]bool{"ready": true}, nil
		})
		return err
	}}
	defer func() {
		if server != nil {
			server.Close()
		}
	}()
	readyTogether := func(want int32) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for range 4 {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- connector.ready(ctx) }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := launches.Load(); got != want {
			t.Fatalf("launches=%d, want=%d", got, want)
		}
	}
	readyTogether(1)
	server.Close()
	server = nil
	readyTogether(2)
}

func TestConnectorFailedLaunchCanRetryWithoutStickyError(t *testing.T) {
	state := t.TempDir()
	var attempts int
	var server *desktopipc.Server
	defer func() {
		if server != nil {
			server.Close()
		}
	}()
	connector := appConnector{state: state, app: "fixture.exe", launch: func(string) error {
		attempts++
		if attempts == 1 {
			return errors.New("transient launch failure")
		}
		var err error
		server, err = desktopipc.Listen(state, func(context.Context, string, json.RawMessage) (any, error) {
			return map[string]bool{"ready": true}, nil
		})
		return err
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := connector.ready(ctx); err == nil {
		t.Fatal("launch failure hidden")
	}
	if err := connector.ready(ctx); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d", attempts)
	}
}

type bridgeChild struct {
	input  io.WriteCloser
	output io.ReadCloser
	done   chan error
	stderr *bytes.Buffer
}

func startBridgeChild(t *testing.T, state string) *bridgeChild {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "DWNL_BRIDGE_TEST_HELPER=1", "DWNL_BRIDGE_TEST_STATE="+state)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	child := &bridgeChild{input: input, output: output, done: make(chan error, 1), stderr: stderr}
	go func() { child.done <- cmd.Wait(); close(child.done) }()
	t.Cleanup(func() {
		_ = input.Close()
		select {
		case <-child.done:
		default:
			_ = cmd.Process.Kill()
			<-child.done
		}
		_ = output.Close()
	})
	return child
}
func awaitChild(t *testing.T, child *bridgeChild, wantError bool) {
	t.Helper()
	select {
	case err := <-child.done:
		if (err != nil) != wantError {
			t.Fatalf("child exit=%v; stderr=%s", err, child.stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge child did not exit")
	}
}
func sendRequest(t *testing.T, out io.Writer, id, method string) {
	t.Helper()
	data, err := json.Marshal(desktopipc.Request{Version: 2, ID: id, Method: method, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = native.WriteFrame(out, data); err != nil {
		t.Fatal(err)
	}
}
func receiveResponse(t *testing.T, in io.Reader) desktopipc.Response {
	t.Helper()
	type outcome struct {
		response desktopipc.Response
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		data, err := native.ReadFrame(in)
		var response desktopipc.Response
		if err == nil {
			err = json.Unmarshal(data, &response)
		}
		done <- outcome{response, err}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result.response
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for framed response")
		return desktopipc.Response{}
	}
}

func TestRealStdinStdoutFramesRemainCorrelatedAndClean(t *testing.T) {
	state := t.TempDir()
	server, err := desktopipc.Listen(state, func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "analyze" {
			time.Sleep(25 * time.Millisecond)
		}
		return map[string]string{"method": method}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	child := startBridgeChild(t, state)
	sendRequest(t, child.input, "slow", "analyze")
	sendRequest(t, child.input, "fast", "snapshot")
	seen := map[string]bool{}
	for range 2 {
		response := receiveResponse(t, child.output)
		if response.Version != 2 || response.Error != nil {
			t.Fatalf("response=%+v", response)
		}
		seen[response.ID] = true
	}
	if !seen["slow"] || !seen["fast"] {
		t.Fatalf("request IDs lost: %v", seen)
	}
	_ = child.input.Close()
	awaitChild(t, child, false)
}

func TestBridgeEOFDoesNotCancelAcceptedApplicationWork(t *testing.T) {
	state := t.TempDir()
	accepted := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	var mutations atomic.Int32
	server, err := desktopipc.Listen(state, func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "enqueue" {
			return map[string]bool{"ready": true}, nil
		}
		mutations.Add(1)
		go func() { <-release; close(completed) }()
		close(accepted)
		<-ctx.Done() // Simulate an accepted mutation whose acknowledgment is lost.
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	child := startBridgeChild(t, state)
	sendRequest(t, child.input, "accepted-once", "enqueue")
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("enqueue not accepted")
	}
	_ = child.input.Close()
	awaitChild(t, child, false)
	select {
	case <-completed:
		t.Fatal("application task finished before its own release")
	default:
	}
	close(release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("application task did not survive bridge EOF")
	}
	if mutations.Load() != 1 {
		t.Fatalf("mutation replayed %d times", mutations.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = desktopipc.Call(ctx, state, desktopipc.Request{Version: 2, ID: "still-alive", Method: "snapshot", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("application stopped on EOF: %v", err)
	}
}

func TestMalformedFramesExitWithoutStartingApplication(t *testing.T) {
	var oversized [4]byte
	binary.LittleEndian.PutUint32(oversized[:], 524289)
	var wrongVersion bytes.Buffer
	_ = native.WriteFrame(&wrongVersion, []byte(`{"version":1,"id":"bad","method":"snapshot","payload":{}}`))
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"truncated-prefix", []byte{1, 2}},
		{"oversized", oversized[:]},
		{"wrong-version", wrongVersion.Bytes()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := startBridgeChild(t, t.TempDir())
			if _, err := child.input.Write(tc.data); err != nil {
				t.Fatal(err)
			}
			_ = child.input.Close()
			awaitChild(t, child, true)
			if strings.Contains(child.stderr.String(), "unexpected launch") {
				t.Fatal("malformed frame launched app")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed Chrome output") }
func TestOutputFailureUnblocksInputAndWaitsForWorkers(t *testing.T) {
	state := t.TempDir()
	server, err := desktopipc.Listen(state, func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]bool{"ready": true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	input, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		done <- serveBridge(state, "unused", input, failingWriter{}, func(string) error { return errors.New("unexpected launch") })
	}()
	sendRequest(t, writer, "broken-output", "snapshot")
	select {
	case err = <-done:
		if err == nil || !strings.Contains(err.Error(), "closed Chrome output") {
			t.Fatalf("output failure lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge remained blocked on input after output failed")
	}
}

func TestInvalidMessageCancelsAndJoinsOutstandingBridgeRequest(t *testing.T) {
	state := t.TempDir()
	entered, canceled := make(chan struct{}), make(chan struct{})
	server, err := desktopipc.Listen(state, func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "analyze" {
			return map[string]bool{"ready": true}, nil
		}
		close(entered)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	child := startBridgeChild(t, state)
	sendRequest(t, child.input, "pending-analysis", "analyze")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("analysis did not enter")
	}
	if err = native.WriteFrame(child.input, []byte(`{"version":2,"id":"invalid","method":"unknown","payload":{}}`)); err != nil {
		t.Fatal(err)
	}
	awaitChild(t, child, true)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("malformed frame left an outstanding bridge request")
	}
}

func TestSameBridgeRelaunchesAfterApplicationCloses(t *testing.T) {
	state := t.TempDir()
	var serverMu sync.Mutex
	var server *desktopipc.Server
	var launches atomic.Int32
	launch := func(string) error {
		serverMu.Lock()
		defer serverMu.Unlock()
		var err error
		server, err = desktopipc.Listen(state, func(context.Context, string, json.RawMessage) (any, error) {
			return map[string]bool{"ready": true}, nil
		})
		if err == nil {
			launches.Add(1)
		}
		return err
	}
	closeServer := func() {
		serverMu.Lock()
		defer serverMu.Unlock()
		if server != nil {
			server.Close()
			server = nil
		}
	}
	defer closeServer()
	input, writer := io.Pipe()
	defer writer.Close()
	output, sink := io.Pipe()
	defer output.Close()
	defer sink.Close()
	done := make(chan error, 1)
	go func() { done <- serveBridge(state, "fixture.exe", input, sink, launch) }()
	sendRequest(t, writer, "first-open", "snapshot")
	if response := receiveResponse(t, output); response.Error != nil || response.ID != "first-open" {
		t.Fatalf("first launch: %+v", response)
	}
	closeServer()
	sendRequest(t, writer, "reopen", "snapshot")
	if response := receiveResponse(t, output); response.Error != nil || response.ID != "reopen" {
		t.Fatalf("second launch: %+v", response)
	}
	if launches.Load() != 2 {
		t.Fatalf("same bridge launched %d times, want 2", launches.Load())
	}
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not close")
	}
}

type observedOutput struct {
	*io.PipeWriter
	entered chan struct{}
	once    sync.Once
}

func (out *observedOutput) Write(data []byte) (int, error) {
	out.once.Do(func() { close(out.entered) })
	return out.PipeWriter.Write(data)
}
func TestEOFUnblocksAResponseWhenChromeStopsReading(t *testing.T) {
	state := t.TempDir()
	server, err := desktopipc.Listen(state, func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]bool{"ready": true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	input, writer := io.Pipe()
	defer writer.Close()
	unread, sink := io.Pipe()
	defer unread.Close()
	output := &observedOutput{PipeWriter: sink, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- serveBridge(state, "unused", input, output, func(string) error { return errors.New("unexpected launch") })
	}()
	sendRequest(t, writer, "unread-response", "snapshot")
	select {
	case <-output.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("response not written")
	}
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge hung on response after EOF")
	}
}
