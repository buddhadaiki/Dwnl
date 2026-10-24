package desktopipc

import (
	"dwnl/host/internal/contracts"
	"encoding/json"
	"unicode/utf8"
)

// Snapshot is a compact status view for the browser. The native app retains
// full URLs, file receipts, legacy history and unabridged descriptions.
// url remains present as an empty string for the existing extension schema;
// the extension uses requestId to reconcile acceptance, never this URL.
type wireTask struct {
	ID          string                 `json:"id"`
	RequestID   string                 `json:"requestId"`
	URL         string                 `json:"url"`
	Title       string                 `json:"title"`
	Mode        contracts.DownloadMode `json:"mode"`
	BitrateKbps int                    `json:"bitrateKbps,omitempty"`
	State       string                 `json:"state"`
	CreatedAt   string                 `json:"createdAt"`
	Error       string                 `json:"error,omitempty"`
	Progress    *contracts.Progress    `json:"progress,omitempty"`
}

type wireSnapshot struct {
	Version      string                 `json:"version"`
	Readiness    string                 `json:"readiness"`
	Capabilities contracts.Capabilities `json:"capabilities"`
	Dependencies []contracts.Dependency `json:"dependencies"`
	Destination  contracts.Destination  `json:"destination"`
	Paused       bool                   `json:"paused"`
	Tasks        []wireTask             `json:"tasks"`
	Error        string                 `json:"error,omitempty"`
	StartAtLogin *bool                  `json:"startAtLogin,omitempty"`
}

func compactSnapshot(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var snapshot wireSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Tasks == nil {
		snapshot.Tasks = []wireTask{}
	}
	if snapshot.Dependencies == nil {
		snapshot.Dependencies = []contracts.Dependency{}
	}
	snapshot.Error = wireText(snapshot.Error)
	for i := range snapshot.Tasks {
		snapshot.Tasks[i].compact()
	}
	return snapshot, nil
}

// enqueue and cancel return one task; it crosses the same browser boundary.
func compactTask(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var task wireTask
	if err = json.Unmarshal(raw, &task); err != nil {
		return nil, err
	}
	task.compact()
	return task, nil
}

func (task *wireTask) compact() {
	task.URL = ""
	task.Title = wireText(task.Title)
	task.Error = wireText(task.Error)
}

// Bound the encoded JSON string, not just rune count. HTML escapes and emoji
// otherwise let 200 legal descriptions exceed the 512 KiB native frame limit.
func wireText(value string) string {
	const maximum = 512
	encoded, _ := json.Marshal(value)
	if len(encoded) <= maximum {
		return value
	}
	const suffix = "…"
	used := 2 + len(suffix) // JSON quotes and UTF-8 ellipsis
	end := 0
	for end < len(value) {
		// Measure the source bytes: an invalid byte is 1 byte but encodes as a 6-byte JSON escape.
		_, size := utf8.DecodeRuneInString(value[end:])
		piece, _ := json.Marshal(value[end : end+size])
		if used+len(piece)-2 > maximum {
			break
		}
		used += len(piece) - 2
		end += size
	}
	return value[:end] + suffix
}
