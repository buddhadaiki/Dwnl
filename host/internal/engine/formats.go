package engine

import (
	"dwnl/host/internal/contracts"
	"encoding/json"
	"fmt"
	"math"
	"slices"
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
	Protocol  string  `json:"protocol"`
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

// Every video download is saved as MP4: the video stream is copied as-is and
// audio that MP4 players do not expect is converted to AAC.
const videoContainer = "mp4"

// Codecs ordered by how widely their MP4 plays on Windows and phones without
// extra installs. rankNotMP4 cannot be copied into MP4 and is re-encoded to
// H.264, so it is offered only when nothing else is.
const (
	rankH264 = iota
	rankAV1
	rankVP9
	rankHEVC
	rankUnknown
	rankNotMP4
)

func videoCodecRank(codec string) int {
	switch probeCodec(codec) {
	case "h264":
		return rankH264
	case "av1":
		return rankAV1
	case "vp9":
		return rankVP9
	case "hevc":
		return rankHEVC
	case "unknown":
		return rankUnknown
	}
	return rankNotMP4
}

// mp4Audio reports audio that MP4 carries without conversion.
func mp4Audio(codec string) bool {
	c := probeCodec(codec)
	return c == "aac" || c == "mp3"
}
func anyAudio(string) bool { return true }

func directProtocol(p string) bool { return p == "" || p == "https" || p == "http" }

// namedSize is the resolution people name: 1080 for 1920×1080, for a
// 1080×1920 Short and for a 1920×800 widescreen crop. Sizes from 720 up snap
// to the standard names, so one name is never offered twice. The app's
// qualityName and the extension's quality.ts label sizes the same way.
func namedSize(f extractedFormat) int {
	size := math.Max(f.Width, f.Height)
	if f.Width > 0 && f.Height > 0 {
		size = math.Max(math.Min(f.Width, f.Height), math.Max(f.Width, f.Height)*9/16)
	}
	for _, standard := range []float64{2160, 1440, 1080, 720} {
		if size >= standard {
			return int(standard)
		}
	}
	return int(math.Round(size))
}

func audioRate(f extractedFormat) float64 {
	if f.ABR <= 0 && !hasCodec(f.VCodec) {
		return f.TBR
	}
	return f.ABR
}

// bestAudioOnly returns the highest-rate audio-only format accepted by the
// filter, preferring the original mix over YouTube's "-drc" variants.
func bestAudioOnly(formats []extractedFormat, accept func(string) bool) *extractedFormat {
	var best *extractedFormat
	for i, f := range formats {
		if f.DRM || hasCodec(f.VCodec) || !hasCodec(f.ACodec) || !accept(f.ACodec) {
			continue
		}
		if best == nil || audioRate(f) > audioRate(*best) || audioRate(f) == audioRate(*best) && strings.HasSuffix(best.ID, "-drc") && !strings.HasSuffix(f.ID, "-drc") {
			best = &formats[i]
		}
	}
	return best
}

type videoPick struct {
	video extractedFormat
	// audio is a separate audio-only track; nil keeps the video's own audio.
	audio *extractedFormat
}

// videoPicks offers one MP4 download per resolution. Extractors list every
// size once per codec, CDN and protocol, which only confuses the choice, so
// each size keeps its most compatible, directly downloadable format.
func videoPicks(formats []extractedFormat) []videoPick {
	hasH264, copyable := false, false
	for _, f := range formats {
		if !f.DRM && hasCodec(f.VCodec) {
			hasH264 = hasH264 || videoCodecRank(f.VCodec) == rankH264
			copyable = copyable || videoCodecRank(f.VCodec) != rankNotMP4
		}
	}
	better := func(a, b extractedFormat) bool {
		if directProtocol(a.Protocol) != directProtocol(b.Protocol) {
			return directProtocol(a.Protocol)
		}
		if ra, rb := videoCodecRank(a.VCodec), videoCodecRank(b.VCodec); ra != rb {
			return ra < rb
		}
		return a.TBR > b.TBR
	}
	bySize := map[int]extractedFormat{}
	known := false
	for _, f := range formats {
		rank := videoCodecRank(f.VCodec)
		// HEVC needs a paid extension on Windows; use it only without H.264.
		if f.DRM || !hasCodec(f.VCodec) || rank == rankNotMP4 && copyable || rank == rankHEVC && hasH264 {
			continue
		}
		size := namedSize(f)
		known = known || size > 0
		if current, ok := bySize[size]; !ok || better(f, current) {
			bySize[size] = f
		}
	}
	sizes := make([]int, 0, len(bySize))
	for size := range bySize {
		if size == 0 && known {
			continue
		}
		sizes = append(sizes, size)
	}
	// Tiny renditions only add noise when 360p or better exists.
	if slices.Max(append([]int{0}, sizes...)) >= 360 {
		sizes = slices.DeleteFunc(sizes, func(size int) bool { return size < 360 })
	}
	slices.Sort(sizes)
	slices.Reverse(sizes)
	readyTrack := bestAudioOnly(formats, mp4Audio) // copied into MP4 as-is
	anyTrack := bestAudioOnly(formats, anyAudio)   // converted to AAC
	picks := make([]videoPick, 0, len(sizes))
	for _, size := range sizes {
		pick := videoPick{video: bySize[size]}
		own := hasCodec(pick.video.ACodec)
		switch {
		case readyTrack != nil && (!own || !mp4Audio(pick.video.ACodec) || audioRate(*readyTrack) > audioRate(pick.video)):
			pick.audio = readyTrack
		case !own && anyTrack != nil:
			pick.audio = anyTrack
		}
		picks = append(picks, pick)
	}
	return picks
}

// pickSize adds the separate audio track to the video size; an unknown part
// leaves the total unknown rather than understating it.
func pickSize(p videoPick) (*uint64, bool) {
	size := func(f extractedFormat) (*uint64, bool) {
		if n := positiveUint(f.Size); n != nil {
			return n, false
		}
		n := positiveUint(f.Estimated)
		return n, n != nil
	}
	total, estimated := size(p.video)
	if total == nil || p.audio == nil {
		return total, estimated
	}
	audio, audioEstimated := size(*p.audio)
	if audio == nil || *total > contracts.MaxSafeInteger-*audio {
		return nil, false
	}
	sum := *total + *audio
	return &sum, estimated || audioEstimated
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
		}
		for _, pick := range videoPicks(item.Formats) {
			f, a := pick.video, pick.video.ACodec
			if pick.audio != nil {
				a = pick.audio.ACodec
			}
			choice := contracts.VideoChoice{OptionID: fmt.Sprintf("option-%d", len(m.VideoChoices)+1), Container: videoContainer, VideoCodec: f.VCodec, HasAudio: hasCodec(a), Width: positiveUint(f.Width), Height: positiveUint(f.Height)}
			if a == "unknown" {
				choice.HasAudio = false
				choice.AudioUnknown = true
			}
			if choice.HasAudio {
				choice.AudioCodec = a
			}
			choice.SizeBytes, choice.SizeEstimated = pickSize(pick)
			m.VideoChoices = append(m.VideoChoices, choice)
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, protocolError(contracts.AccessRestricted, "No public media item was returned.")
	}
	return out, nil
}
