// Package desktopipc transports bounded, versioned control messages only.
package desktopipc

import (
	"bytes"
	"context"
	"dwnl/host/internal/contracts"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const Version = 2

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var methods = map[string]bool{"snapshot": true, "analyze": true, "listFormats": true, "enqueue": true, "cancel": true, "continue": true, "chooseDestination": true, "openApp": true, "openFile": true, "openFolder": true, "settings": true, "archiveHistory": true, "renameTask": true}

// History edits move or drop the user's records, so only the window may ask.
var appOnly = map[string]string{"archiveHistory": "Arquive o histórico pela janela do aplicativo Dwnl.", "renameTask": "Renomeie downloads pela janela do aplicativo Dwnl."}

type Request struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Response struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}
type Handler func(context.Context, string, json.RawMessage) (any, error)

type requestIDKey struct{}

// RequestID names the pipe request a handler is serving; it is empty for
// calls that did not arrive through the pipe.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func Decode(data []byte) (Request, error) {
	var r Request
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if len(data) > contracts.MaxMessageBytes || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.Version != Version || !idPattern.MatchString(r.ID) || !methods[r.Method] || len(r.Payload) == 0 || r.Payload[0] != '{' || !json.Valid(r.Payload) {
		return r, errors.New("Mensagem do aplicativo inválida")
	}
	return r, nil
}
func Handle(ctx context.Context, h Handler, r Request) Response {
	if message, ok := appOnly[r.Method]; ok {
		return Response{Version: Version, ID: r.ID, Error: &Error{Code: "APP_ONLY", Message: message}}
	}
	result, err := h(context.WithValue(ctx, requestIDKey{}, r.ID), r.Method, r.Payload)
	if err == nil && (r.Method == "snapshot" || r.Method == "continue" || r.Method == "settings") {
		result, err = compactSnapshot(result)
	}
	if err == nil && (r.Method == "enqueue" || r.Method == "cancel") {
		result, err = compactTask(result)
	}
	response := Response{Version: Version, ID: r.ID, Result: result}
	if err != nil {
		response.Result = nil
		var typed *contracts.ProtocolError
		code := "APP_ERROR"
		if errors.As(err, &typed) {
			code = string(typed.Code)
		}
		response.Error = &Error{Code: code, Message: wireText(err.Error())}
	}
	return response
}
