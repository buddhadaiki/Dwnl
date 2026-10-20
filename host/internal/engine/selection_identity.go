package engine

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"encoding/hex"
	"encoding/json"
)

// SelectionIdentity is a private, stable digest for durable queue recovery. It
// binds the canonical source, actual extractor item and stream IDs and their
// codec/dimension semantics; ephemeral UI IDs and estimated sizes are excluded.
// An empty optionID identifies the source audio selected for MP3 conversion.
func (e *Engine) SelectionIdentity(analysisID, itemID, optionID string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	a, ok := e.analyses[analysisID]
	if !ok || a.online == nil {
		return "", protocolError(contracts.AnalysisExpired, "Analyze this source again before saving it.")
	}
	item, ok := a.online.Items[itemID]
	if !ok {
		return "", protocolError(contracts.InvalidSelection, "The selected source item is unavailable.")
	}
	videoID, audioID := "", item.AudioID
	if optionID != "" {
		pick, exists := item.Choices[optionID]
		if !exists {
			return "", protocolError(contracts.InvalidSelection, "The selected source format is unavailable.")
		}
		videoID, audioID = pick.VideoID, pick.AudioID
	}
	normalized := func(id string) extractedFormat {
		for _, f := range item.Record.Formats {
			if f.ID == id {
				f.Size = 0
				f.Estimated = 0
				return f
			}
		}
		return extractedFormat{}
	}
	b, err := json.Marshal(struct {
		Source, Item, Mode string
		Video, Audio       extractedFormat
	}{
		a.analysis.SourceURL, item.ID, map[bool]string{true: "video", false: "audio"}[optionID != ""], normalized(videoID), normalized(audioID),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
