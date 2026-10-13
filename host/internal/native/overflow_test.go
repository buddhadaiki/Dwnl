package native

import (
	"context"
	"dwnl/host/internal/contracts"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"
)

func TestControlOverflowFailsClosedAndStopsInflightAnalysis(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	dest, err := storage.NewDestinations(filepath.Join(base, "downloads"), nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := &waitingAnalysisEngine{begun: make(chan struct{}), stopped: make(chan struct{})}
	extension := "lphdonkmiecidfgjfplhdfnpphmdioeh"
	session, err := hostruntime.NewSession(hostruntime.Options{SessionID: hostruntime.NewID(), Engine: engine, Store: store, Destinations: dest, Origin: "chrome-extension://" + extension + "/", ExtensionID: extension, Handshake: contracts.HandshakeResult{HostVersion: "0.1.0", EngineVersion: "0.1.0", Dependencies: []contracts.Dependency{}}})
	if err != nil {
		t.Fatal(err)
	}
	input, writer := io.Pipe()
	outputReader, output := io.Pipe()
	defer input.Close()
	defer writer.Close()
	defer outputReader.Close()
	defer output.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Serve(ctx, input, output, session) }()
	write := func(req contracts.Request) error { b, _ := json.Marshal(req); return WriteFrame(writer, b) }
	if err = write(contracts.Request{ProtocolVersion: 1, RequestID: "hello", Operation: contracts.OperationHandshake, Payload: json.RawMessage(`{"extensionVersion":"0.1.0"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadFrame(outputReader); err != nil {
		t.Fatal(err)
	}
	if err = write(contracts.Request{ProtocolVersion: 1, RequestID: "analyze", HostSessionID: session.ID(), Operation: contracts.OperationAnalyze, Payload: json.RawMessage(`{"url":"https://www.youtube.com/watch?v=fixture"}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.begun:
	case <-time.After(5 * time.Second):
		t.Fatal("analysis did not begin")
	}
	writes := make(chan error, 1)
	go func() {
		for i := 0; i < 17; i++ {
			if err := write(contracts.Request{ProtocolVersion: 1, RequestID: hostruntime.NewID(), HostSessionID: session.ID(), Operation: contracts.OperationStatus, Payload: json.RawMessage(`{}`)}); err != nil {
				writes <- err
				return
			}
		}
		writes <- nil
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrControlOverflow) {
			t.Fatalf("overflow did not fail closed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("control overflow left a busy handler and reader alive")
	}
	select {
	case <-engine.stopped:
	default:
		t.Fatal("overflow returned before owned analysis stopped")
	}
	select {
	case err := <-writes:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reader blocked instead of consuming the overflow frame")
	}
}
