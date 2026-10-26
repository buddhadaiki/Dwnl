package engine

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/testutil"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type downloadBoundaryRunner struct {
	local    *realRunner
	metadata []byte
	media    LocalMedia
	formats  []string
}

func (r *downloadBoundaryRunner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	if s.Executable != ports.YTDLP {
		return r.local.Run(ctx, s, emit)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "metadata.json"), r.metadata, 0600); err != nil {
		return ports.ProcessResult{}, err
	}
	for i, arg := range s.Args {
		if arg == "--format" {
			id := s.Args[i+1]
			r.formats = append(r.formats, id)
			guard, err := os.ReadFile(filepath.Join(s.Dir, "selection.json"))
			if err != nil {
				return ports.ProcessResult{}, err
			}
			if !strings.Contains(string(guard), `"id":"owned"`) {
				return ports.ProcessResult{}, os.ErrInvalid
			}
			source := r.media.VideoPath
			if id == "a" {
				source = r.media.AudioPath
			}
			body, err := os.ReadFile(source)
			if err != nil {
				return ports.ProcessResult{}, err
			}
			if err = os.WriteFile(filepath.Join(s.Dir, "downloaded.bin"), body, 0600); err != nil {
				return ports.ProcessResult{}, err
			}
		}
	}
	return ports.ProcessResult{}, nil
}
func TestOnlineSelectionDownloadsExactFormatsThenPhysicallyMuxes(t *testing.T) {
	local := mediaRunner(t)
	media := generateFixture(t, local)
	raw := []byte(`{"entries":[{"id":"owned","title":"Owned generated public-boundary fixture","duration":2,"formats":[{"format_id":"v","ext":"mp4","vcodec":"avc1.640028","acodec":"none","width":160,"height":90},{"format_id":"a","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","abr":128}]}]}`)
	r := &downloadBoundaryRunner{local: local, metadata: raw, media: media}
	root := testutil.TempDir(t)
	e, _ := New(Config{HostSessionID: "online-session", Runner: r, PrivateBinDir: local.bin, AnalysisRoot: filepath.Join(root, "analyses")})
	e.capabilities = contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}
	a, err := e.Analyze(context.Background(), "https://www.youtube.com/watch?v=owned")
	if err != nil {
		t.Fatal(err)
	}
	plan := localPlan(t, a, contracts.ModeVideo)
	result, err := e.Execute(context.Background(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.formats, ",") != "v,a" {
		t.Fatalf("download selection %v", r.formats)
	}
	proof, err := e.probeMedia(context.Background(), result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if proof.stream("video").CodecName != "h264" || proof.stream("audio").CodecName != "aac" {
		t.Fatalf("streams %+v", proof.Streams)
	}
	bytes, err := os.ReadFile(result.Path)
	if err != nil || uint64(len(bytes)) != result.SizeBytes || result.SHA256 == "" {
		t.Fatalf("physical final %+v %v", result, err)
	}
	_, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(plan.WorkDir)
	if len(entries) != 0 {
		t.Fatalf("owned download work remained: %v", entries)
	}
}

// TikTok reports whole seconds: a 2 s clip may be listed as 3, but not as 4.
func TestWholeSecondDurationsAllowTikTokRounding(t *testing.T) {
	for _, tc := range []struct {
		reported string
		ok       bool
	}{{"3", true}, {"4", false}, {"2.6", false}} {
		t.Run(tc.reported, func(t *testing.T) {
			local := mediaRunner(t)
			raw := []byte(`{"entries":[{"id":"owned","title":"Rounded duration fixture","duration":` + tc.reported + `,"formats":[{"format_id":"v","ext":"mp4","vcodec":"avc1.640028","acodec":"none","width":160,"height":90},{"format_id":"a","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","abr":128}]}]}`)
			r := &downloadBoundaryRunner{local: local, metadata: raw, media: generateFixture(t, local)}
			root := testutil.TempDir(t)
			e, _ := New(Config{HostSessionID: "online-session", Runner: r, PrivateBinDir: local.bin, AnalysisRoot: filepath.Join(root, "analyses")})
			e.capabilities = contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}
			a, err := e.Analyze(context.Background(), "https://www.tiktok.com/@owned/video/123")
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.Execute(context.Background(), localPlan(t, a, contracts.ModeVideo), nil)
			if tc.ok && err != nil {
				t.Fatal(err)
			}
			if !tc.ok {
				code(t, err, contracts.InvalidOutput)
			}
		})
	}
	for reported, want := range map[float64]float64{10: 1, 10.5: .5, 300: 3, 30.25: .5} {
		if got := durationTolerance(reported); got != want {
			t.Errorf("durationTolerance(%v) = %v, want %v", reported, got, want)
		}
	}
}

type metadataRunner struct {
	specs []ports.ExecSpec
	raw   []byte
}

func (r *metadataRunner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	r.specs = append(r.specs, s)
	if ctx.Err() != nil {
		return ports.ProcessResult{Canceled: true}, ctx.Err()
	}
	if s.Executable != ports.YTDLP {
		return ports.ProcessResult{ExitCode: 1}, nil
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "metadata.json"), r.raw, 0600); err != nil {
		return ports.ProcessResult{}, err
	}
	return ports.ProcessResult{}, nil
}
func TestOnlineAnalyzeUsesPrivateAdapterAndOffersActualMultiItems(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/ytdlp-formats.json")
	if err != nil {
		t.Fatal(err)
	}
	root := testutil.TempDir(t)
	bin := filepath.Join(root, "bin")
	analysisRoot := filepath.Join(root, "analyses")
	if err = os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(analysisRoot, 0700); err != nil {
		t.Fatal(err)
	}
	r := &metadataRunner{raw: raw}
	e, _ := New(Config{HostSessionID: "online-session", Runner: r, PrivateBinDir: bin, AnalysisRoot: analysisRoot})
	analysis, err := e.Analyze(context.Background(), "https://x.com/owned/status/123")
	if err != nil || len(analysis.Items) != 2 {
		t.Fatalf("real analysis path: %+v %v", analysis, err)
	}
	if len(r.specs) != 1 || len(r.specs[0].Env) == 0 {
		t.Fatal("extractor operation missing or inherited environment")
	}
	args := strings.Join(r.specs[0].Args, "\n")
	if !strings.Contains(args, "^Dwnl$") || !strings.Contains(args, "--no-cookies") || !strings.Contains(args, "--no-geo-bypass") || !strings.Contains(args, "--no-remote-components") {
		t.Fatalf("unsafe extractor args: %v", r.specs[0].Args)
	}
	if !strings.Contains(args, "youtube:player_client=visionos,web;fetch_pot=never") {
		t.Fatalf("extractor used obsolete or unrestricted YouTube clients: %v", r.specs[0].Args)
	}
	entries, err := os.ReadDir(analysisRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("analysis tokens/temporary state retained: %v %v", entries, err)
	}
	e.capabilities.Video = true
	selected := analysis.Items[1].VideoChoices[0]
	_, _, err = e.selection(contracts.DownloadPlan{HostSessionID: "online-session", Analysis: analysis, Item: analysis.Items[1], JobID: "job", DestinationID: "approved", Mode: contracts.ModeVideo, VideoChoice: &selected})
	if err != nil {
		t.Fatalf("second post item could not be selected: %v", err)
	}
}

func TestYouTubePublicClientGrammarRejectsObsoleteOrUnboundedClients(t *testing.T) {
	root := testutil.TempDir(t)
	for _, client := range []string{"android_vr", "default", "all", "web_embedded", "visionos,web,web_embedded"} {
		t.Run(client, func(t *testing.T) {
			args := onlineArgs(root, root)
			for index, value := range args {
				if strings.HasPrefix(value, "youtube:player_client=") {
					args[index] = "youtube:player_client=" + client + ";fetch_pot=never"
				}
			}
			args = append(args, "--extractor-args", "dwnl:mode=probe", "--skip-download", "--", "https://www.youtube.com/watch?v=dwnl-private-self-test")
			if err := ValidateExecSpec(ports.ExecSpec{Executable: ports.YTDLP, Args: args, Dir: root}); err == nil {
				t.Fatalf("accepted unsupported YouTube client set %q", client)
			}
		})
	}
}

func TestOnlineUnknownAudioIsConfirmedFromPhysicalFile(t *testing.T) {
	for _, withAudio := range []bool{false, true} {
		t.Run(fmt.Sprint(withAudio), func(t *testing.T) {
			local := mediaRunner(t)
			media := generateFixture(t, local)
			if withAudio {
				merged := filepath.Join(testutil.TempDir(t), "av.mp4")
				out, err := exec.Command(filepath.Join(local.bin, ports.ExecutableFile(ports.FFmpeg)), "-nostdin", "-v", "error", "-i", media.VideoPath, "-i", media.AudioPath, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", merged).CombinedOutput()
				if err != nil {
					t.Fatalf("fixture merge %v %s", err, out)
				}
				media.VideoPath = merged
			}
			r := &downloadBoundaryRunner{local: local, media: media, metadata: []byte(`{"entries":[{"id":"owned","title":"Owned unknown-codec fixture","duration":2,"formats":[{"format_id":"v","vcodec":"unknown","acodec":"unknown","width":160,"height":90}]}]}`)}
			root := testutil.TempDir(t)
			e, _ := New(Config{HostSessionID: "online-session", Runner: r, PrivateBinDir: local.bin, AnalysisRoot: filepath.Join(root, "analyses")})
			e.capabilities = contracts.Capabilities{Video: true, MP3: true, NetworkAccess: true}
			a, err := e.Analyze(context.Background(), "https://www.youtube.com/watch?v=owned")
			if err != nil {
				t.Fatal(err)
			}
			if a.Items[0].HasAudio || !a.Items[0].AudioUnknown || a.Items[0].VideoChoices[0].HasAudio || !a.Items[0].VideoChoices[0].AudioUnknown {
				t.Fatalf("invented audio metadata: %+v", a.Items[0])
			}
			result, err := e.Execute(context.Background(), localPlan(t, a, contracts.ModeVideo), nil)
			if err != nil {
				t.Fatal(err)
			}
			p, err := e.probeMedia(context.Background(), result.Path)
			if err != nil {
				t.Fatal(err)
			}
			if (p.stream("audio") != nil) != withAudio {
				t.Fatalf("physical audio %+v", p.Streams)
			}
			_, err = e.Execute(context.Background(), localPlan(t, a, contracts.ModeMP3), nil)
			if withAudio {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				code(t, err, contracts.NoAudio)
			}
		})
	}
}
func TestOnlineAnalyzeCancelFailedPreservesOwnedWork(t *testing.T) {
	root := testutil.TempDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e, _ := New(Config{HostSessionID: "session", Runner: cancelFailureRunner{cancel: cancel}, PrivateBinDir: filepath.Join(root, "bin"), AnalysisRoot: filepath.Join(root, "analyses")})
	_, err := e.Analyze(ctx, "https://www.youtube.com/watch?v=owned")
	code(t, err, contracts.CancelFailed)
	entries, err := os.ReadDir(filepath.Join(root, "analyses"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("work deleted without confirmed termination: %v %v", entries, err)
	}
}
func TestOnlinePrivateAdapterProbeIsOfflineAndReal(t *testing.T) {
	r := mediaRunner(t)
	root := testutil.TempDir(t)
	e, _ := New(Config{HostSessionID: "session", Runner: r, PrivateBinDir: r.bin, AnalysisRoot: filepath.Join(root, "analyses")})
	if err := e.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !e.Capabilities().NetworkAccess {
		t.Fatal("embedded guard was not loaded by real yt-dlp")
	}
	for _, s := range r.specs {
		for _, arg := range s.Args {
			if strings.HasPrefix(arg, "https://") && arg != "https://www.youtube.com/watch?v=dwnl-private-self-test" {
				t.Fatalf("probe touched a site %s", arg)
			}
		}
	}
}

func TestMissingPrivateAdapterCannotFallBackToNativeSiteExtractor(t *testing.T) {
	r := mediaRunner(t)
	dir := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "plugins", "dwnl", "yt_dlp_plugins", "extractor"), 0700); err != nil {
		t.Fatal(err)
	}
	args := append(onlineArgs(dir, r.bin), "--extractor-args", "dwnl:mode=probe", "--skip-download", "--", "https://www.youtube.com/watch?v=dwnl-private-self-test")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var output strings.Builder
	result, err := r.Run(ctx, ports.ExecSpec{Executable: ports.YTDLP, Dir: dir, Env: onlineEnvironment(dir, r.bin), Args: args}, func(line ports.ProcessLine) { output.WriteString(line.Text); output.WriteByte('\n') })
	if err != nil || result.ExitCode == 0 || !strings.Contains(strings.ToLower(output.String()), "no suitable extractor") || strings.Contains(strings.ToLower(output.String()), "downloading webpage") || strings.Contains(output.String(), "DWNL_ADAPTER_READY") {
		t.Fatalf("missing adapter did not fail closed: %v %+v %s", err, result, output.String())
	}
}
func TestAudioSourceUsesKnownTotalBitrateForAudioOnlyVariants(t *testing.T) {
	var item extractedItem
	if err := json.Unmarshal([]byte(`{"formats":[{"format_id":"a32","vcodec":"none","acodec":"unknown","tbr":32},{"format_id":"a128","vcodec":"none","acodec":"unknown","tbr":128},{"format_id":"av2000","vcodec":"h264","acodec":"aac","tbr":2000}]}`), &item); err != nil {
		t.Fatal(err)
	}
	if got := audioFormat(item.Formats); got != "a128" {
		t.Fatalf("selected %s; highest reported audio-only bitrate is a128", got)
	}
}
func TestVideoChoiceUsesBestSeparateAudioInsteadOfLowerProgressiveAudio(t *testing.T) {
	for _, tc := range []struct {
		name, separate, wantAudio, wantCodec string
	}{
		{"better AAC replaces progressive AAC", "aac", "best-audio", "aac"},
		// Opus would need converting to fit MP4; the clip's own AAC is kept.
		{"opus does not replace progressive AAC", "opus", "combined", "aac"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"entries":[{"id":"owned","title":"Public fixture","formats":[{"format_id":"combined","vcodec":"h264","acodec":"aac","abr":64},{"format_id":"best-audio","vcodec":"none","acodec":"` + tc.separate + `","abr":128}]}]}`)
			root := testutil.TempDir(t)
			r := &metadataRunner{raw: raw}
			e, _ := New(Config{HostSessionID: "session", Runner: r, PrivateBinDir: filepath.Join(root, "bin"), AnalysisRoot: filepath.Join(root, "analyses")})
			a, err := e.Analyze(context.Background(), "https://x.com/owned/status/123")
			if err != nil {
				t.Fatal(err)
			}
			choice := a.Items[0].VideoChoices[0]
			pick := e.analyses[a.AnalysisID].online.Items[a.Items[0].ItemID].Choices[choice.OptionID]
			if choice.AudioCodec != tc.wantCodec || choice.Container != "mp4" || pick.VideoID != "combined" || pick.AudioID != tc.wantAudio {
				t.Fatalf("choice=%+v privatePick=%+v", choice, pick)
			}
		})
	}
}
func TestReplacedProgressiveAudioDoesNotExposeOriginalFormatSize(t *testing.T) {
	for _, size := range []string{`"filesize":100000`, `"filesize_approx":100000`} {
		raw := []byte(`{"id":"owned","title":"Public fixture","formats":[{"format_id":"combined","vcodec":"h264","acodec":"aac","abr":64,` + size + `},{"format_id":"best-audio","vcodec":"none","acodec":"aac","abr":128}]}`)
		items, err := ParseMetadata(raw)
		if err != nil {
			t.Fatal(err)
		}
		choice := items[0].VideoChoices[0]
		if choice.SizeBytes != nil || choice.SizeEstimated {
			t.Fatalf("size of original progressive format exposed after audio replacement: %+v", choice)
		}
	}
}
