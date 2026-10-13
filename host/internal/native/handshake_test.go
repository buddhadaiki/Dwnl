package native

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	"dwnl/host/internal/contracts"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
)

func TestEofCancelsHandshakeDependencyProbe(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	dest, err := storage.NewDestinations(filepath.Join(base, "downloads"), nil)
	if err != nil {
		t.Fatal(err)
	}
	begun, stopped := make(chan struct{}), make(chan struct{})
	extension := "lphdonkmiecidfgjfplhdfnpphmdioeh"
	session, err := hostruntime.NewSession(hostruntime.Options{
		SessionID: hostruntime.NewID(), Engine: &waitingAnalysisEngine{}, Store: store, Destinations: dest,
		Origin: "chrome-extension://" + extension + "/", ExtensionID: extension,
		LoadHandshake: func(ctx context.Context) contracts.HandshakeResult {
			close(begun)
			<-ctx.Done()
			close(stopped)
			return contracts.HandshakeResult{HostVersion: "0.1.0", EngineVersion: "0.1.0", Dependencies: []contracts.Dependency{}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, writer := io.Pipe()
	defer input.Close()
	var output bytes.Buffer
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { result <- Serve(ctx, input, &output, session) }()
	request, _ := json.Marshal(contracts.Request{ProtocolVersion: 1, RequestID: "hello", Operation: contracts.OperationHandshake, Payload: json.RawMessage(`{"extensionVersion":"0.1.0"}`)})
	if err := WriteFrame(writer, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-begun:
	case <-time.After(5 * time.Second):
		t.Fatal("dependency probe did not begin")
	}
	queued, _ := json.Marshal(contracts.Request{ProtocolVersion: 1, RequestID: "queued-status", HostSessionID: session.ID(), Operation: contracts.OperationStatus, Payload: json.RawMessage(`{}`)})
	if err := WriteFrame(writer, queued); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EOF left dependency probe running")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("host returned before dependency probe stopped")
	}
	if output.Len() != 0 {
		t.Fatal("host wrote a response after disconnect")
	}
}
