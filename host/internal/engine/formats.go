package engine

import (
	"dwnl/host/internal/contracts"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

type extractedFormat struct {
	ID        string  `json:"format_id"`
	Ext       string  `json:"ext"`
	VCodec    string  `json:"vcodec"`
	ACodec    string  `json:"acodec"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	Size      float64 `json:"filesize"`
	Estimated float64 `json:"filesize_approx"`
	ABR       float64 `json:"abr"`
	TBR       float64 `json:"tbr"`
	DRM       bool    `json:"has_drm"`
}
type extractedItem struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Duration     float64           `json:"duration"`
	Availability string            `json:"availability"`
	DRM          bool              `json:"has_drm"`
	Formats      []extractedFormat `json:"formats"`
	Entries      []json.RawMessage `json:"entries"`
}

func hasCodec(c string) bool { return c != "" && c != "none" }
func positiveUint(v float64) *uint64 {
	if v <= 0 || v > float64(contracts.MaxSafeInteger) || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	n := uint64(v)
	if float64(n) != v {
		return nil
	}
	return &n
}
func containerFor(v, a string) string {
	if (strings.HasPrefix(v, "avc1") || v == "h264" || strings.HasPrefix(v, "hev1") || v == "hevc" || strings.HasPrefix(v, "av01") || v == "av1") && (!hasCodec(a) || a == "aac" || strings.HasPrefix(a, "mp4a")) {
		return "mp4"
	}
	if (v == "vp8" || v == "vp9" || strings.HasPrefix(v, "vp09") || v == "av1" || strings.HasPrefix(v, "av01")) && (!hasCodec(a) || a == "opus" || a == "vorbis") {
		return "webm"
	}
	return "mkv"
}

// ParseMetadata translates bounded structured yt-dlp output only; it neither
// imports stream URLs nor makes metadata sufficient authority to download.
func ParseMetadata(raw []byte) ([]contracts.MediaItem, error) {
	if len(raw) > 2*1024*1024 || !utf8.Valid(raw) {
		return nil, protocolError(contracts.EngineError, "Extractor metadata is invalid or exceeds the supported limit.")
	}
	var root extractedItem
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, protocolError(contracts.EngineError, "Extractor metadata is malformed.")
	}
	records := []extractedItem{root}
	if root.Entries != nil {
		records = nil
		for _, entry := range root.Entries {
			if string(entry) == "null" {
				continue
			}
			var item extractedItem
			if err := json.Unmarshal(entry, &item); err != nil {
				return nil, protocolError(contracts.EngineError, "An extractor item is malformed.")
			}
			records = append(records, item)
			if len(records) > 64 {
				return nil, protocolError(contracts.EngineError, "This source contains too many media items.")
			}
		}
	}
	out := make([]contracts.MediaItem, 0, len(records))
	for i, item := range records {
		if item.DRM || item.Availability == "private" || item.Availability == "premium_only" || item.Availability == "subscriber_only" || item.Availability == "needs_auth" {
			return nil, protocolError(contracts.AccessRestricted, "This media requires access that Dwnl does not request or bypass.")
		}
		if item.ID == "" || len(item.Formats) > 4096 {
			return nil, protocolError(contracts.EngineError, "The extractor did not provide a usable media item.")
		}
		title := item.Title
		omitted := false
		if title == "" {
			title = "Media"
		}
		if len(title) > 4096 {
			for len(title) > 4096 {
				_, n := utf8.DecodeLastRuneInString(title)
				title = title[:len(title)-n]
			}
			omitted = true
		}
		audio := ""
		bestAudio := audioFormat(item.Formats)
		for _, f := range item.Formats {
			if f.ID == bestAudio {
				audio = f.ACodec
				break
			}
		}
		m := contracts.MediaItem{ItemID: fmt.Sprintf("item-%d", i+1), Title: title, VideoChoices: []contracts.VideoChoice{}, OmittedMetadata: omitted}
		if item.Duration > 0 && !math.IsInf(item.Duration, 0) {
			duration := item.Duration
			m.DurationSeconds = &duration
		}
		for _, f := range item.Formats {
			if f.DRM {
				continue
			}
			if hasCodec(f.ACodec) {
				if f.ACodec == "unknown" {
					m.AudioUnknown = true
				} else {
					m.HasAudio = true
				}
			}
			if !hasCodec(f.VCodec) {
				continue
			}
			a := f.ACodec
			if hasCodec(audio) {
				a = audio
			}
			choice := contracts.VideoChoice{OptionID: fmt.Sprintf("option-%d", len(m.VideoChoices)+1), Container: containerFor(f.VCodec, a), VideoCodec: f.VCodec, HasAudio: hasCodec(a), Width: positiveUint(f.Width), Height: positiveUint(f.Height)}
			if a == "unknown" {
				choice.HasAudio = false
				choice.AudioUnknown = true
			}
			if choice.HasAudio {
				choice.AudioCodec = a
			}
			choice.SizeBytes = positiveUint(f.Size)
			if choice.SizeBytes == nil {
				choice.SizeBytes = positiveUint(f.Estimated)
				choice.SizeEstimated = choice.SizeBytes != nil
			}
			if hasCodec(a) && bestAudio != f.ID {
				choice.SizeBytes = nil
				choice.SizeEstimated = false
			}
			m.VideoChoices = append(m.VideoChoices, choice)
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, protocolError(contracts.AccessRestricted, "No public media item was returned.")
	}
	return out, nil
}
