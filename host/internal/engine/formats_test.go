package engine

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/testutil"
	"path/filepath"
	"testing"
)

// Real TikTok listing: H.264 at 576p on two CDNs and three bitrates, plus
// ByteDance HEVC (reported as h265) at 576p and 720p, all with their own AAC.
const tiktokFormats = `{"id":"owned","title":"TikTok fixture","duration":10.5,"formats":[
{"format_id":"h264_540p_992286-0","ext":"mp4","vcodec":"h264","acodec":"aac","width":576,"height":1024,"tbr":992,"filesize":1307586,"protocol":"https"},
{"format_id":"h264_540p_992286-1","ext":"mp4","vcodec":"h264","acodec":"aac","width":576,"height":1024,"tbr":992,"filesize":1307586,"protocol":"https"},
{"format_id":"h264_540p_2240963-0","ext":"mp4","vcodec":"h264","acodec":"aac","width":576,"height":1024,"tbr":2240,"filesize":2953029,"protocol":"https"},
{"format_id":"h264_540p_2240963-1","ext":"mp4","vcodec":"h264","acodec":"aac","width":576,"height":1024,"tbr":2240,"filesize":2953029,"protocol":"https"},
{"format_id":"bytevc1_540p_1297374-0","ext":"mp4","vcodec":"h265","acodec":"aac","width":576,"height":1024,"tbr":1297,"filesize":1728265,"protocol":"https"},
{"format_id":"bytevc1_720p_1504834-0","ext":"mp4","vcodec":"h265","acodec":"aac","width":720,"height":1280,"tbr":1504,"filesize":2004627,"protocol":"https"}]}`

// Real YouTube listing (trimmed): every size in H.264, VP9 and AV1, HLS
// duplicates with higher bitrates, and AAC/Opus audio with "-drc" variants.
const youtubeFormats = `{"id":"owned","title":"YouTube fixture","duration":634.5,"formats":[
{"format_id":"139-drc","vcodec":"none","acodec":"mp4a.40.5","abr":48.7,"filesize":3871021,"protocol":"https"},
{"format_id":"140-drc","vcodec":"none","acodec":"mp4a.40.2","abr":129.4,"filesize":10271496,"protocol":"https"},
{"format_id":"251","vcodec":"none","acodec":"opus","abr":128.6,"filesize":10202210,"protocol":"https"},
{"format_id":"140","vcodec":"none","acodec":"mp4a.40.2","abr":129.4,"filesize":10271496,"protocol":"https"},
{"format_id":"160","vcodec":"avc1.4D400C","acodec":"none","width":256,"height":144,"tbr":54,"filesize":4323893,"protocol":"https"},
{"format_id":"134","vcodec":"avc1.4D401E","acodec":"none","width":640,"height":360,"tbr":230,"filesize":18294110,"protocol":"https"},
{"format_id":"243","vcodec":"vp9","acodec":"none","width":640,"height":360,"tbr":303,"filesize":24109536,"protocol":"https"},
{"format_id":"396","vcodec":"av01.0.01M.08","acodec":"none","width":640,"height":360,"tbr":192,"filesize":15298808,"protocol":"https"},
{"format_id":"311","vcodec":"avc1.4D4020","acodec":"none","width":1280,"height":720,"tbr":4553,"protocol":"m3u8_native"},
{"format_id":"298","vcodec":"avc1.4D4020","acodec":"none","width":1280,"height":720,"tbr":1897,"filesize":150524867,"protocol":"https"},
{"format_id":"302","vcodec":"vp9","acodec":"none","width":1280,"height":720,"tbr":1420,"filesize":112676322,"protocol":"https"},
{"format_id":"299","vcodec":"avc1.64002A","acodec":"none","width":1920,"height":1080,"tbr":3247,"filesize":257619653,"protocol":"https"},
{"format_id":"399","vcodec":"av01.0.09M.08","acodec":"none","width":1920,"height":1080,"tbr":1568,"filesize":124386876,"protocol":"https"},
{"format_id":"308","vcodec":"vp9","acodec":"none","width":2560,"height":1440,"tbr":5967,"filesize":473363704,"protocol":"https"},
{"format_id":"400","vcodec":"av01.0.12M.08","acodec":"none","width":2560,"height":1440,"tbr":3940,"filesize":312554313,"protocol":"https"},
{"format_id":"315","vcodec":"vp9","acodec":"none","width":3840,"height":2160,"tbr":17174,"filesize":1362269481,"protocol":"https"},
{"format_id":"401","vcodec":"av01.0.13M.08","acodec":"none","width":3840,"height":2160,"tbr":8981,"filesize":712445280,"protocol":"https"}]}`

type wantPick struct {
	video, audio string
	height       uint64
}

func analyzeFixture(t *testing.T, raw string) (contracts.MediaItem, map[string]remotePick) {
	t.Helper()
	root := testutil.TempDir(t)
	e, _ := New(Config{HostSessionID: "session", Runner: &metadataRunner{raw: []byte(raw)}, PrivateBinDir: filepath.Join(root, "bin"), AnalysisRoot: filepath.Join(root, "analyses")})
	a, err := e.Analyze(context.Background(), "https://www.youtube.com/watch?v=owned")
	if err != nil {
		t.Fatal(err)
	}
	return a.Items[0], e.analyses[a.AnalysisID].online.Items[a.Items[0].ItemID].Choices
}

func checkPicks(t *testing.T, item contracts.MediaItem, picks map[string]remotePick, want []wantPick) {
	t.Helper()
	if len(item.VideoChoices) != len(want) {
		t.Fatalf("got %d choices, want %d: %+v", len(item.VideoChoices), len(want), item.VideoChoices)
	}
	for i, w := range want {
		c, p := item.VideoChoices[i], picks[item.VideoChoices[i].OptionID]
		if c.Container != "mp4" || c.Height == nil || *c.Height != w.height || p.VideoID != w.video || p.AudioID != w.audio || !c.HasAudio {
			t.Errorf("choice %d = %+v pick=%+v; want %+v", i, c, p, w)
		}
	}
}

func TestTikTokOffersH264MP4WithItsOwnAudio(t *testing.T) {
	item, picks := analyzeFixture(t, tiktokFormats)
	// HEVC needs a paid Windows extension; H.264 exists, so HEVC is not offered.
	checkPicks(t, item, picks, []wantPick{{"h264_540p_2240963-0", "h264_540p_2240963-0", 1024}})
	c := item.VideoChoices[0]
	if c.VideoCodec != "h264" || c.AudioCodec != "aac" || c.SizeBytes == nil || *c.SizeBytes != 2953029 {
		t.Fatalf("choice=%+v", c)
	}
}

func TestYouTubeOffersOneMP4PerResolution(t *testing.T) {
	item, picks := analyzeFixture(t, youtubeFormats)
	// Above 1080p only AV1/VP9 exist; AV1 plays more widely. 720p skips the
	// HLS rendition, and 144p is dropped once 360p or better exists.
	checkPicks(t, item, picks, []wantPick{{"401", "140", 2160}, {"400", "140", 1440}, {"299", "140", 1080}, {"298", "140", 720}, {"134", "140", 360}})
	c := item.VideoChoices[2]
	if c.AudioCodec != "mp4a.40.2" || c.SizeBytes == nil || *c.SizeBytes != 257619653+10271496 {
		t.Fatalf("1080p choice=%+v", c)
	}
}

func TestHEVCIsOfferedWhenNoH264Exists(t *testing.T) {
	item, picks := analyzeFixture(t, `{"id":"owned","formats":[{"format_id":"hevc","vcodec":"hvc1.1.6.L93","acodec":"aac","width":1280,"height":720}]}`)
	checkPicks(t, item, picks, []wantPick{{"hevc", "hevc", 720}})
}

func TestOpusOnlyAudioIsPairedForConversion(t *testing.T) {
	item, picks := analyzeFixture(t, `{"id":"owned","formats":[{"format_id":"v","vcodec":"vp9","acodec":"none","width":1280,"height":720},{"format_id":"a","vcodec":"none","acodec":"opus","abr":160}]}`)
	checkPicks(t, item, picks, []wantPick{{"v", "a", 720}})
	if item.VideoChoices[0].AudioCodec != "opus" {
		t.Fatalf("choice=%+v", item.VideoChoices[0])
	}
}

func TestUnsizedFormatIsKeptOnlyWithoutSizedAlternatives(t *testing.T) {
	items, err := ParseMetadata([]byte(`{"id":"owned","formats":[{"format_id":"v","vcodec":"avc1","acodec":"aac"},{"format_id":"vp8","vcodec":"vp8","acodec":"vorbis","height":720}]}`))
	if err != nil {
		t.Fatal(err)
	}
	// VP8 cannot be copied into MP4; the unsized H.264 is the only option.
	if c := items[0].VideoChoices; len(c) != 1 || c[0].Height != nil || c[0].VideoCodec != "avc1" {
		t.Fatalf("choices=%+v", c)
	}
}

func TestSizesAreGroupedByTheResolutionPeopleName(t *testing.T) {
	item, picks := analyzeFixture(t, `{"id":"owned","formats":[
{"format_id":"crop","vcodec":"avc1","acodec":"aac","width":1920,"height":800,"tbr":3000},
{"format_id":"full","vcodec":"avc1","acodec":"aac","width":1920,"height":1080,"tbr":4000},
{"format_id":"tall","vcodec":"avc1","acodec":"aac","width":1920,"height":1200,"tbr":2000},
{"format_id":"sd","vcodec":"avc1","acodec":"aac","width":1024,"height":576,"tbr":900}]}`)
	// A widescreen crop is still 1080p, and 1920x1200 adds no second "1080p".
	checkPicks(t, item, picks, []wantPick{{"full", "full", 1080}, {"sd", "sd", 576}})
}

func TestVP8IsOfferedOnlyWithoutMP4CompatibleVideo(t *testing.T) {
	item, picks := analyzeFixture(t, `{"id":"owned","formats":[{"format_id":"old","vcodec":"vp8","acodec":"vorbis","width":640,"height":360}]}`)
	checkPicks(t, item, picks, []wantPick{{"old", "old", 360}})
}
