package desktopipc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"dwnl/host/internal/testutil"
)

func TestClientDisconnectCancelsHandler(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "state")
	started, finished := make(chan struct{}), make(chan struct{})
	server, err := Listen(root, func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := make(chan error, 1)
	go func() {
		_, err := Call(ctx, root, Request{Version: Version, ID: "disconnect", Method: "analyze", Payload: json.RawMessage(`{"url":"https://www.youtube.com/shorts/owned"}`)})
		response <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("disconnected client left handler running")
	}
	select {
	case err := <-response:
		if err == nil {
			t.Fatal("canceled client returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("client call did not finish")
	}
}

func TestServerCloseCancelsAndWaitsForHandlers(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "state")
	started, finished := make(chan struct{}), make(chan struct{})
	server, err := Listen(root, func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	callDone := make(chan struct{})
	go func() {
		defer close(callDone)
		_, _ = Call(ctx, root, Request{Version: Version, ID: "shutdown", Method: "chooseDestination", Payload: json.RawMessage(`{}`)})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		server.Close()
		t.Fatal("handler did not start")
	}
	closed := make(chan struct{})
	go func() { server.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("server shutdown did not cancel handler")
	}
	select {
	case <-finished:
	default:
		t.Fatal("server returned before handler finished")
	}
	<-callDone
}

func TestPeerMonitorPreservesNormalResponse(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "state")
	server, err := Listen(root, func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]string{"version": "0.2.0", "readiness": "ready"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := Call(ctx, root, Request{Version: Version, ID: "roundtrip", Method: "snapshot", Payload: json.RawMessage(`{}`)})
	if err != nil || response.Error != nil {
		t.Fatalf("response: %+v, %v", response, err)
	}
	got, ok := response.Result.(map[string]any)
	if !ok || got["readiness"] != "ready" {
		t.Fatalf("result: %#v", response.Result)
	}
}
