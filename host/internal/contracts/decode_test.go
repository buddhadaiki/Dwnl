package contracts

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// These regressions catch a decoder that rejects legitimate protocol traffic.
func TestDecodeValidHandshake(t *testing.T) {
	req, err := DecodeRequest([]byte(`{"protocolVersion":1,"requestId":"request_1","operation":"handshake","payload":{"extensionVersion":"0.1.0"}}`))
	if err != nil {
		t.Fatalf("valid handshake rejected: %v", err)
	}
	if req.RequestID != "request_1" || req.Operation != "handshake" {
		t.Fatalf("unexpected request: %+v", req)
	}
}
func TestDecodeValidReadyEvent(t *testing.T) {
	msg, err := DecodeMessage([]byte(`{"protocolVersion":1,"kind":"event","hostSessionId":"session_1","jobId":"job_1","sequence":0,"state":"ready"}`))
	if err != nil {
		t.Fatalf("valid ready event rejected: %v", err)
	}
	if msg.Event == nil || msg.Event.JobID != "job_1" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

// Every assertion is hand-defined in the shared corpus, independent of either decoder.
func TestSharedCorpus(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Context ValidationContext `json:"context"`
		Cases   []struct {
			Name       string          `json:"name"`
			Direction  string          `json:"direction"`
			Valid      bool            `json:"valid"`
			ErrorCode  ErrorCode       `json:"errorCode"`
			UseContext bool            `json:"useContext"`
			Value      json.RawMessage `json:"value"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, tc := range corpus.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			var err error
			if tc.Direction == "request" {
				var req Request
				req, err = DecodeRequest(tc.Value)
				if err == nil && tc.UseContext {
					err = ValidateRequestContext(req, corpus.Context)
				}
			} else {
				_, err = DecodeMessage(tc.Value)
			}
			if tc.Valid {
				if err != nil {
					t.Fatalf("valid fixture rejected: %v", err)
				}
				return
			}
			var decodeErr *ProtocolDecodeError
			if !errors.As(err, &decodeErr) {
				t.Fatalf("expected %s typed error, got %v", tc.ErrorCode, err)
			}
			if decodeErr.Code != tc.ErrorCode {
				t.Fatalf("expected %s, got %s", tc.ErrorCode, decodeErr.Code)
			}
		})
	}
}

func TestRejectOversizedFrame(t *testing.T) {
	_, err := DecodeRequest([]byte(strings.Repeat(" ", MaxMessageBytes+1)))
	var decodeErr *ProtocolDecodeError
	if !errors.As(err, &decodeErr) || decodeErr.Code != MessageTooLarge {
		t.Fatalf("expected MESSAGE_TOO_LARGE, got %v", err)
	}
}

func TestJSONFramingBoundaries(t *testing.T) {
	base := `{"protocolVersion":1,"requestId":"request_1","operation":"handshake","payload":{"extensionVersion":"é"}}`
	t.Run("exact byte limit accepts UTF8", func(t *testing.T) {
		frame := base + strings.Repeat(" ", MaxMessageBytes-len([]byte(base)))
		if _, err := DecodeRequest([]byte(frame)); err != nil {
			t.Fatal(err)
		}
	})
	for name, frame := range map[string][]byte{
		"malformed":       []byte(`{"protocolVersion":`),
		"trailing object": []byte(base + ` {}`),
		"invalid UTF8":    append([]byte(base[:len(base)-3]), 0xff, '"', '}', '}'),
	} {
		t.Run(name, func(t *testing.T) { _, err := DecodeRequest(frame); expectCode(t, err, InvalidMessage) })
	}
}

func TestContextExpiryAndAudio(t *testing.T) {
	ctx := ValidationContext{HostSessionID: "session-a", Now: "2026-10-13T12:00:00Z", Analysis: &Analysis{AnalysisID: "analysis-a", HostSessionID: "session-a", ExpiresAt: "2026-10-13T12:00:00Z", Items: []MediaItem{{ItemID: "item-a", HasAudio: true}}}}
	req, err := DecodeRequest([]byte(`{"protocolVersion":1,"requestId":"req-a","hostSessionId":"session-a","operation":"start","payload":{"analysisId":"analysis-a","itemId":"item-a","mode":"mp3","bitrateKbps":192,"destinationId":"destination-a"}}`))
	if err != nil {
		t.Fatal(err)
	}
	expectCode(t, ValidateRequestContext(req, ctx), AnalysisExpired)
	ctx.Analysis.ExpiresAt = "2026-10-13T12:01:00Z"
	ctx.Analysis.Items[0].HasAudio = false
	expectCode(t, ValidateRequestContext(req, ctx), NoAudio)
}

func expectCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	var decodeErr *ProtocolDecodeError
	if !errors.As(err, &decodeErr) || decodeErr.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestStrictMediaMetadata(t *testing.T) {
	for name, frame := range map[string]string{
		"codec cannot claim absent audio": `{"protocolVersion":1,"kind":"response","requestId":"req-a","hostSessionId":"session-a","operation":"listFormats","result":{"analysisId":"analysis-a","itemId":"item-a","choices":[{"optionId":"option-a","container":"mp4","videoCodec":"h264","audioCodec":"aac","hasAudio":false,"sizeEstimated":false}],"omittedMetadata":false}}`,
		"estimated size needs bytes":      `{"protocolVersion":1,"kind":"response","requestId":"req-a","hostSessionId":"session-a","operation":"listFormats","result":{"analysisId":"analysis-a","itemId":"item-a","choices":[{"optionId":"option-a","container":"mp4","videoCodec":"h264","hasAudio":false,"sizeEstimated":true}],"omittedMetadata":false}}`,
		"MP3 cannot claim video codec":    `{"protocolVersion":1,"kind":"event","hostSessionId":"session-a","jobId":"job-a","sequence":9,"state":"completed","completedFile":{"path":"C:\\Downloads\\fixture.mp3","format":"mp3","sizeBytes":12800,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","audioCodec":"mp3","videoCodec":"h264","mp3BitrateKbps":192}}`,
	} {
		t.Run(name, func(t *testing.T) { _, err := DecodeMessage([]byte(frame)); expectCode(t, err, InvalidMessage) })
	}
}
