package desktopipc

import (
	"dwnl/host/internal/contracts"
	"encoding/json"
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
		task := &snapshot.Tasks[i]
		task.URL = ""
		task.Title = wireText(task.Title)
		task.Error = wireText(task.Error)
	}
	return snapshot, nil
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
	for offset, r := range value {
		piece, _ := json.Marshal(string(r))
		if used+len(piece)-2 > maximum {
			break
		}
		used += len(piece) - 2
		end = offset + len(string(r))
	}
	return value[:end] + suffix
}
