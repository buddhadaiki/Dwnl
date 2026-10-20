package engine

import (
	"dwnl/host/internal/contracts"
	"testing"
)

func identityFixture() *Engine {
	format := extractedFormat{ID: "137", VCodec: "avc1.640028", ACodec: "none", Width: 1080, Height: 1920, Size: 100}
	audio := extractedFormat{ID: "140", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128}
	item := remoteItem{ID: "source", AudioID: "140", Record: extractedItem{Formats: []extractedFormat{format, audio}}, Choices: map[string]remotePick{"option": {VideoID: "137", AudioID: "140"}}}
	a := localAnalysis{analysis: contracts.Analysis{SourceURL: "https://www.youtube.com/shorts/source"}, online: &onlineAnalysis{Items: map[string]remoteItem{"item": item}}}
	return &Engine{analyses: map[string]localAnalysis{"a": a}}
}
func TestSelectionIdentitySurvivesNewUIIdentifiersAndEstimatedSizes(t *testing.T) {
	e := identityFixture()
	first, err := e.SelectionIdentity("a", "item", "option")
	if err != nil {
		t.Fatal(err)
	}
	a := e.analyses["a"]
	item := a.online.Items["item"]
	pick := item.Choices["option"]
	delete(item.Choices, "option")
	item.Choices["new-option"] = pick
	item.Record.Formats[0].Size = 999
	item.Record.Formats[0].Estimated = 1000
	delete(a.online.Items, "item")
	a.online.Items["new-item"] = item
	delete(e.analyses, "a")
	e.analyses["new-analysis"] = a
	next, err := e.SelectionIdentity("new-analysis", "new-item", "new-option")
	if err != nil || first != next {
		t.Fatalf("identity changed for UI-only IDs: %q %q %v", first, next, err)
	}
}
func TestSelectionIdentityBindsActualItemStreamsAndQuality(t *testing.T) {
	for _, change := range []string{"source", "item", "video", "audio", "codec", "dimensions"} {
		t.Run(change, func(t *testing.T) {
			e := identityFixture()
			first, _ := e.SelectionIdentity("a", "item", "option")
			a := e.analyses["a"]
			item := a.online.Items["item"]
			switch change {
			case "source":
				a.analysis.SourceURL = "https://www.youtube.com/shorts/other"
			case "item":
				item.ID = "other-item"
			case "video":
				pick := item.Choices["option"]
				pick.VideoID = "other-video"
				item.Choices["option"] = pick
			case "audio":
				pick := item.Choices["option"]
				pick.AudioID = "other-audio"
				item.Choices["option"] = pick
			case "codec":
				item.Record.Formats[0].VCodec = "vp9"
			case "dimensions":
				item.Record.Formats[0].Width = 720
			}
			a.online.Items["item"] = item
			e.analyses["a"] = a
			changed, err := e.SelectionIdentity("a", "item", "option")
			if err != nil {
				t.Fatal(err)
			}
			if changed == first {
				t.Fatal("different selection matched original")
			}
		})
	}
}
func TestSelectionIdentityRejectsUnknownAndSeparatesAudio(t *testing.T) {
	e := identityFixture()
	if _, err := e.SelectionIdentity("missing", "item", "option"); err == nil {
		t.Fatal("unknown analysis accepted")
	}
	if _, err := e.SelectionIdentity("a", "item", "missing"); err == nil {
		t.Fatal("unknown choice accepted")
	}
	video, _ := e.SelectionIdentity("a", "item", "option")
	audio, _ := e.SelectionIdentity("a", "item", "")
	if video == audio {
		t.Fatal("audio and video identity collide")
	}
}
