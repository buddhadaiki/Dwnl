package desktopipc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"dwnl/host/internal/contracts"
)

func TestCompactSnapshotWith200FullSizeTasks(t *testing.T) {
	for _, description := range []string{strings.Repeat("x", 4096), strings.Repeat("<", 4096), strings.Repeat("😀", 1024)} {
		t.Run(fmt.Sprint(len(description), "-", description[:1]), func(t *testing.T) {
			tasks := make([]map[string]any, 200)
			for i := range tasks {
				tasks[i] = map[string]any{"id": fmt.Sprintf("task-%d", i), "requestId": fmt.Sprintf("request-%d", i), "url": "https://www.youtube.com/shorts/" + strings.Repeat("v", 4000), "title": description, "mode": "video", "state": "failed", "createdAt": "2026-10-19T12:00:00Z", "error": description, "completedFile": map[string]any{"path": strings.Repeat("p", 4096)}}
			}
			source := map[string]any{"version": "0.2.0", "readiness": "ready", "capabilities": contracts.Capabilities{Video: true}, "dependencies": []contracts.Dependency{}, "destination": contracts.Destination{DestinationID: "destination", Label: `C:\Downloads`}, "paused": true, "tasks": tasks, "error": description, "legacyHistory": []any{map[string]any{"large": strings.Repeat("h", 500000)}}, "startAtLogin": true}
			original, _ := json.Marshal(source)
			if len(original) <= contracts.MaxMessageBytes {
				t.Fatal("fixture does not exceed native limit")
			}
			for _, method := range []string{"snapshot", "continue", "settings"} {
				response := Handle(context.Background(), func(context.Context, string, json.RawMessage) (any, error) { return source, nil }, Request{Version: Version, ID: "volume", Method: method})
				if response.Error != nil {
					t.Fatal(response.Error)
				}
				encoded, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				if len(encoded) > contracts.MaxMessageBytes {
					t.Fatalf("%s snapshot has %d bytes", method, len(encoded))
				}
				var wire struct {
					Result map[string]json.RawMessage `json:"result"`
				}
				if err = json.Unmarshal(encoded, &wire); err != nil {
					t.Fatal(err)
				}
				if _, ok := wire.Result["legacyHistory"]; ok {
					t.Fatal("legacy history leaked into compact snapshot")
				}
				var rows []map[string]any
				if err = json.Unmarshal(wire.Result["tasks"], &rows); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 200 {
					t.Fatal("task receipts were removed")
				}
				for i, row := range rows {
					if row["requestId"] != fmt.Sprintf("request-%d", i) || row["state"] != "failed" || row["mode"] != "video" || row["createdAt"] != "2026-10-19T12:00:00Z" || row["url"] != "" {
						t.Fatalf("extension-required fields missing: %+v", row)
					}
					if _, ok := row["completedFile"]; ok {
						t.Fatal("file receipt leaked")
					}
					for _, field := range []string{"title", "error"} {
						text := row[field].(string)
						b, _ := json.Marshal(text)
						if len(b) > 512 || !utf8.ValidString(text) {
							t.Fatal("description exceeds wire bound")
						}
					}
				}
				if string(wire.Result["startAtLogin"]) != "true" {
					t.Fatal("setting lost")
				}
			}
			after, _ := json.Marshal(source)
			if string(original) != string(after) {
				t.Fatal("projection mutated full app state")
			}
		})
	}
}

func TestCompactProjectionAppliesToTaskReceipts(t *testing.T) {
	for _, method := range []string{"enqueue", "cancel"} {
		full := map[string]any{"title": strings.Repeat("x", 4096), "url": "https://www.youtube.com/shorts/source", "completedFile": map[string]any{"path": "C:/Downloads/one.mp4"}}
		response := Handle(context.Background(), func(context.Context, string, json.RawMessage) (any, error) { return full, nil }, Request{Method: method, ID: method})
		data, _ := json.Marshal(response.Result)
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		if row["url"] != "" || row["completedFile"] != nil || len(row["title"].(string)) > 512 {
			t.Fatalf("%s receipt crossed the browser boundary unprojected: %s", method, data)
		}
	}
}

func TestWireTextBoundsInvalidUTF8(t *testing.T) {
	for n := 80; n <= 180; n++ {
		value := strings.Repeat("\xff", n)
		encoded, _ := json.Marshal(wireText(value))
		if len(encoded) > 512 {
			t.Fatalf("%d invalid bytes encoded to %d bytes", n, len(encoded))
		}
	}
}
