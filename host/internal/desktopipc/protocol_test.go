package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeBoundaries(t *testing.T) {
	for _, s := range []string{`{"version":2,"id":"one","method":"snapshot","payload":{}}`, `{"version":2,"id":"job-1","method":"enqueue","payload":{}}`} {
		if _, e := Decode([]byte(s)); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{`{"version":1,"id":"one","method":"snapshot","payload":{}}`, `{"version":2,"id":"../one","method":"snapshot","payload":{}}`, `{"version":2,"id":"one","method":"exec","payload":{}}`, `{"version":2,"id":"one","method":"snapshot","payload":null}`, `{"version":2,"id":"one","method":"snapshot","payload":{},"extra":1}`, `{"version":2,"id":"one","method":"snapshot","payload":{}} {}`} {
		if _, e := Decode([]byte(s)); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestErrorNeverReturnsSuccess(t *testing.T) {
	r := Handle(context.Background(), func(context.Context, string, json.RawMessage) (any, error) { return "partial", errors.New("falha") }, Request{ID: "one"})
	if r.Result != nil || r.Error == nil || r.ID != "one" {
		t.Fatal(r)
	}
}

func TestArchiveHistoryValidatesLocallyButCannotRunOverIPC(t *testing.T) {
	req, err := Decode([]byte(`{"version":2,"id":"archive","method":"archiveHistory","payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	response := Handle(context.Background(), func(context.Context, string, json.RawMessage) (any, error) { called = true; return nil, nil }, req)
	if called || response.Error == nil || response.Error.Code != "APP_ONLY" {
		t.Fatalf("archive escaped local app boundary: %+v", response)
	}
}
