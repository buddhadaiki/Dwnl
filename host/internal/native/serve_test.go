package native

import (
	"context"
	"dwnl/host/internal/contracts"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"
)

type waitingAnalysisEngine struct {
	begun   chan struct{}
	stopped chan struct{}
}

func (e *waitingAnalysisEngine) Analyze(ctx context.Context, url string) (contracts.Analysis, error) {
	close(e.begun)
	<-ctx.Done()
	close(e.stopped)
	return contracts.Analysis{}, ctx.Err()
}
func (e *waitingAnalysisEngine) Execute(context.Context, contracts.DownloadPlan, func(contracts.Progress)) (contracts.CompletedFile, error) {
	panic("unexpected execution")
}
func TestEofCancelsInflightAnalysisBeforeServeReturns(t *testing.T) {
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
	input, inputWriter := io.Pipe()
	outputReader, output := io.Pipe()
	defer input.Close()
	defer outputReader.Close()
	defer output.Close()
	result := make(chan error, 1)
	go func() { result <- Serve(context.Background(), input, output, session) }()
	send := func(request contracts.Request) {
		b, _ := json.Marshal(request)
		if err := WriteFrame(inputWriter, b); err != nil {
			t.Fatal(err)
		}
	}
	send(contracts.Request{ProtocolVersion: 1, RequestID: "hello", Operation: contracts.OperationHandshake, Payload: json.RawMessage(`{"extensionVersion":"0.1.0"}`)})
	response, err := ReadFrame(outputReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = contracts.DecodeMessage(response); err != nil {
		t.Fatal(err)
	}
	send(contracts.Request{ProtocolVersion: 1, RequestID: "analyze", HostSessionID: session.ID(), Operation: contracts.OperationAnalyze, Payload: json.RawMessage(`{"url":"https://www.youtube.com/watch?v=fixture"}`)})
	select {
	case <-engine.begun:
	case <-time.After(5 * time.Second):
		t.Fatal("analysis did not begin")
	}
	inputWriter.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EOF did not finish the host")
	}
	select {
	case <-engine.stopped:
	default:
		t.Fatal("Serve returned while owned analysis still ran")
	}
}
