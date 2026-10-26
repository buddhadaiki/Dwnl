package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/testutil"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This runner invokes fixed private binaries without a shell. Lifecycle/Job
// Object behavior is tested in T3; these tests exercise real media processing.
type realRunner struct {
	bin   string
	specs []ports.ExecSpec
}

func (r *realRunner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	r.specs = append(r.specs, s)
	cmd := exec.CommandContext(ctx, filepath.Join(r.bin, ports.ExecutableFile(s.Executable)), s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if emit != nil {
		for _, stream := range []struct {
			b *bytes.Buffer
			s ports.ProcessStream
		}{{&stdout, ports.Stdout}, {&stderr, ports.Stderr}} {
			scanner := bufio.NewScanner(stream.b)
			scanner.Buffer(make([]byte, 4096), ports.MaxProcessLineBytes)
			for scanner.Scan() {
				emit(ports.ProcessLine{Stream: stream.s, Text: scanner.Text()})
			}
			if scanner.Err() != nil {
				return ports.ProcessResult{}, scanner.Err()
			}
		}
	}
	if ctx.Err() != nil {
		return ports.ProcessResult{Canceled: true}, ctx.Err()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ports.ProcessResult{ExitCode: ee.ExitCode()}, nil
	}
	return ports.ProcessResult{}, err
}
func mediaRunner(t *testing.T) *realRunner {
	t.Helper()
	bin := os.Getenv("DWNL_MEDIA_BIN")
	if bin == "" {
		bin = filepath.Join("..", "..", "..", ".tools", "media", "bin")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bin, ports.ExecutableFile(ports.FFmpeg))); err != nil {
		t.Skip("private media tools unavailable: " + err.Error())
	}
	return &realRunner{bin: bin}
}
func generateFixture(t *testing.T, r *realRunner) LocalMedia {
	t.Helper()
	dir := testutil.TempDir(t)
	video := filepath.Join(dir, "video.mp4")
	audio := filepath.Join(dir, "audio.m4a")
	for _, a := range [][]string{{"-nostdin", "-hide_banner", "-loglevel", "error", "-n", "-f", "lavfi", "-i", "color=c=black:size=160x90:rate=25,drawbox=color=white:t=fill:enable='between(t,0.5,0.54)'", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an", video}, {"-nostdin", "-hide_banner", "-loglevel", "error", "-n", "-f", "lavfi", "-i", "aevalsrc=if(between(t\\,0.5\\,0.54)\\,0.3*sin(2*PI*1000*t)\\,0):s=44100", "-t", "2", "-c:a", "aac", audio}} {
		out, err := exec.Command(filepath.Join(r.bin, ports.ExecutableFile(ports.FFmpeg)), a...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture generation: %v %s", err, out)
		}
	}
	return LocalMedia{VideoPath: video, AudioPath: audio, Title: "Owned local fixture"}
}
func audioOnset(t *testing.T, r *realRunner, path string) float64 {
	t.Helper()
	cmd := exec.Command(filepath.Join(r.bin, ports.ExecutableFile(ports.FFmpeg)), "-nostdin", "-v", "error", "-i", path, "-map", "0:a:0", "-f", "f32le", "-ac", "1", "-ar", "44100", "pipe:1")
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+4 <= len(data); i += 4 {
		sample := math.Float32frombits(binary.LittleEndian.Uint32(data[i : i+4]))
		if math.Abs(float64(sample)) > .05 {
			return float64(i/4) / 44100
		}
	}
	t.Fatal("fixture beep was not decoded")
	return 0
}
func TestAVFixtureSynchronization(t *testing.T) {
	e, r, a := fixtureEngine(t)
	result, err := e.Execute(context.Background(), localPlan(t, a, contracts.ModeVideo), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command(filepath.Join(r.bin, ports.ExecutableFile(ports.FFmpeg)), "-nostdin", "-v", "error", "-i", result.Path, "-map", "0:v:0", "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	videoTime := -1.0
	const pixels = 160 * 90
	for offset := 0; offset+pixels <= len(data); offset += pixels {
		if data[offset] > 200 {
			videoTime = float64(offset/pixels) / 25
			break
		}
	}
	audioTime := audioOnset(t, r, result.Path)
	if videoTime < .48 || videoTime > .56 || math.Abs(audioTime-.5) > .05 || math.Abs(videoTime-audioTime) > .1 {
		t.Fatalf("flash=%fs beep=%fs; synchronization must be <=100ms", videoTime, audioTime)
	}
	t.Logf("decoded flash at %.6fs; beep at %.6fs; A/V delta %.3fms", videoTime, audioTime, math.Abs(videoTime-audioTime)*1000)
}

const fixtureURL = "https://www.youtube.com/watch?v=dwnl-local-fixture"

func fixtureEngine(t *testing.T) (*Engine, *realRunner, contracts.Analysis) {
	t.Helper()
	r := mediaRunner(t)
	e, err := New(Config{HostSessionID: "local-session", Runner: r})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, err := e.ImportLocal(context.Background(), fixtureURL, generateFixture(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 1 || len(a.Items[0].VideoChoices) != 1 {
		t.Fatalf("local analysis=%+v", a)
	}
	return e, r, a
}
func localPlan(t *testing.T, a contracts.Analysis, mode contracts.DownloadMode) contracts.DownloadPlan {
	t.Helper()
	if len(a.Items) != 1 || (mode == contracts.ModeVideo && len(a.Items[0].VideoChoices) != 1) {
		t.Fatalf("missing local fixture analysis: %+v", a)
	}
	var choice *contracts.VideoChoice
	if mode == contracts.ModeVideo {
		c := a.Items[0].VideoChoices[0]
		choice = &c
	}
	return contracts.DownloadPlan{RequestID: "start", HostSessionID: a.HostSessionID, JobID: "job", Analysis: a, Item: a.Items[0], Mode: mode, VideoChoice: choice, MP3BitrateKbps: 192, DestinationID: "destination", DestinationPath: testutil.TempDir(t), WorkDir: testutil.TempDir(t)}
}
func TestFixturesVideoStreamcopy(t *testing.T) {
	e, r, a := fixtureEngine(t)
	plan := localPlan(t, a, contracts.ModeVideo)
	var stages []contracts.JobState
	result, err := e.Execute(context.Background(), plan, func(p contracts.Progress) { stages = append(stages, p.Stage) })
	if err != nil {
		t.Fatal(err)
	}
	if result.VideoCodec != "h264" || result.AudioCodec != "aac" || result.Format != "mp4" || result.SizeBytes == 0 || len(result.SHA256) != 64 {
		t.Fatalf("result=%+v", result)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if result.SHA256 != hex.EncodeToString(sum[:]) || uint64(len(data)) != result.SizeBytes {
		t.Fatal("physical output hash/size mismatch")
	}
	if !reflect.DeepEqual(stages[:1], []contracts.JobState{contracts.StateMuxing}) || stages[len(stages)-1] != contracts.StateSaving {
		t.Fatalf("stages=%v", stages)
	}
	copyCalled := false
	for _, s := range r.specs {
		if s.Executable == ports.FFmpeg {
			for i := 0; i+1 < len(s.Args); i++ {
				if s.Args[i] == "-c" && s.Args[i+1] == "copy" {
					copyCalled = true
					t.Logf("actual streamcopy command: ffmpeg %s", strings.Join(s.Args, " "))
				}
			}
		}
	}
	if !copyCalled {
		t.Fatal("video was not streamcopied")
	}
	t.Logf("physically verified remux: %+v", result)
	out, err := exec.Command(filepath.Join(r.bin, ports.ExecutableFile(ports.FFmpeg)), "-nostdin", "-v", "error", "-i", result.Path, "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("full A/V decode failed: %v %s", err, out)
	}
}
func TestMp3128192320(t *testing.T) {
	e, r, a := fixtureEngine(t)
	for _, bitrate := range []int{128, 192, 320} {
		t.Run(fmt.Sprint(bitrate), func(t *testing.T) {
			plan := localPlan(t, a, contracts.ModeMP3)
			plan.VideoChoice = nil
			plan.MP3BitrateKbps = bitrate
			result, err := e.Execute(context.Background(), plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			onset := audioOnset(t, r, result.Path)
			if math.Abs(onset-.5) > .1 {
				t.Fatalf("MP3 beep shifted to %fs", onset)
			}
			if result.Format != "mp3" || result.AudioCodec != "mp3" || result.VideoCodec != "" || result.MP3BitrateKbps != bitrate {
				t.Fatalf("result=%+v", result)
			}
			out, err := exec.Command(filepath.Join(r.bin, ports.ExecutableFile(ports.FFmpeg)), "-nostdin", "-v", "error", "-i", result.Path, "-f", "null", "-").CombinedOutput()
			if err != nil {
				t.Fatalf("MP3 decode: %v %s", err, out)
			}
			t.Logf("physical MP3: %+v", result)
		})
	}
}
func TestStaleSelectionRejected(t *testing.T) {
	e, r, a := fixtureEngine(t)
	before := len(r.specs)
	plan := localPlan(t, a, contracts.ModeVideo)
	plan.VideoChoice.OptionID = "other-option"
	_, err := e.Execute(context.Background(), plan, nil)
	code(t, err, contracts.InvalidSelection)
	plan = localPlan(t, a, contracts.ModeVideo)
	plan.HostSessionID = "other-session"
	_, err = e.Execute(context.Background(), plan, nil)
	code(t, err, contracts.SessionEnded)
	plan = localPlan(t, a, contracts.ModeVideo)
	stored := e.analyses[a.AnalysisID]
	stored.analysis.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	e.analyses[a.AnalysisID] = stored
	_, err = e.Execute(context.Background(), plan, nil)
	code(t, err, contracts.AnalysisExpired)
	if len(r.specs) != before {
		t.Fatal("invalid selection started media subprocess")
	}
}
func TestNoAudioRejectsMp3(t *testing.T) {
	r := mediaRunner(t)
	e, _ := New(Config{HostSessionID: "session", Runner: r})
	media := generateFixture(t, r)
	media.AudioPath = ""
	a, err := e.ImportLocal(context.Background(), fixtureURL, media)
	if err != nil {
		t.Fatal(err)
	}
	plan := localPlan(t, a, contracts.ModeMP3)
	plan.VideoChoice = nil
	_, err = e.Execute(context.Background(), plan, nil)
	code(t, err, contracts.NoAudio)
}
func TestMissingMp3EncoderIsCapabilityError(t *testing.T) {
	e, r, a := fixtureEngine(t)
	e.config.Runner = maskMp3Runner{next: r}
	if err := e.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !e.Capabilities().Video || e.Capabilities().MP3 {
		t.Fatalf("capabilities=%+v", e.Capabilities())
	}
	plan := localPlan(t, a, contracts.ModeMP3)
	plan.VideoChoice = nil
	_, err := e.Execute(context.Background(), plan, nil)
	code(t, err, contracts.MP3Unavailable)
}

// The available build includes libmp3lame. Mask its reported encoder at the
// external process boundary to exercise the unavailable-build path honestly.
type maskMp3Runner struct{ next ports.ProcessRunner }

func (r maskMp3Runner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	return r.next.Run(ctx, s, func(line ports.ProcessLine) {
		if s.Executable == ports.FFmpeg && strings.Contains(strings.Join(s.Args, " "), "-encoders") && strings.Contains(line.Text, "libmp3lame") {
			return
		}
		if emit != nil {
			emit(line)
		}
	})
}
func TestAudioOnlyLocalFixture(t *testing.T) {
	r := mediaRunner(t)
	e, _ := New(Config{HostSessionID: "audio-session", Runner: r})
	if err := e.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	media := generateFixture(t, r)
	media.VideoPath = ""
	a, err := e.ImportLocal(context.Background(), fixtureURL, media)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Items[0].HasAudio || len(a.Items[0].VideoChoices) != 0 {
		t.Fatalf("invented video: %+v", a)
	}
	result, err := e.Execute(context.Background(), localPlan(t, a, contracts.ModeMP3), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AudioCodec != "mp3" || result.VideoCodec != "" {
		t.Fatalf("result=%+v", result)
	}
}
func TestReservedAndTraversalTitlesCannotEscapeDestination(t *testing.T) {
	source := filepath.Join(testutil.TempDir(t), "source")
	if err := os.WriteFile(source, []byte("owned bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"CON", "NUL .txt", "COM¹", `../../escaped`, `C:\\absolute\\name`, "....", ""} {
		dir := testutil.TempDir(t)
		result, err := finalize(context.Background(), source, dir, title, "mp4", "job", contracts.CompletedFile{}, nil)
		if err != nil {
			t.Fatalf("title %q: %v", title, err)
		}
		if filepath.Dir(result.Path) != dir {
			t.Fatalf("escaped title %q: %s", title, result.Path)
		}
		data, err := os.ReadFile(result.Path)
		if err != nil || string(data) != "owned bytes" {
			t.Fatalf("title %q did not make a regular file", title)
		}
	}
}
func TestDestinationNoOverwriteAndOwnedIntermediate(t *testing.T) {
	dir := testutil.TempDir(t)
	sourceDir := testutil.TempDir(t)
	source := filepath.Join(sourceDir, "source.bin")
	if err := os.WriteFile(source, []byte("new media bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	preexisting := filepath.Join(dir, "Owned media.mp4")
	if err := os.WriteFile(preexisting, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	done, err := finalize(context.Background(), source, dir, "Owned media", "mp4", "job", contracts.CompletedFile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(done.Path) != dir || filepath.Base(done.Path) != "Owned media (1).mp4" {
		t.Fatalf("unsafe final path=%s", done.Path)
	}
	existing, _ := os.ReadFile(preexisting)
	if string(existing) != "preserve" {
		t.Fatal("overwrote existing file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("destination residual files=%v", entries)
	}
	t.Logf("destination finalized by intermediate copy: %s", done.Path)
}
func TestLocalSourceChangedRequiresNewAnalysis(t *testing.T) {
	e, _, a := fixtureEngine(t)
	stored := e.analyses[a.AnalysisID]
	file, err := os.OpenFile(stored.media.VideoPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte("changed after analysis"))
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	_, err = e.Execute(context.Background(), localPlan(t, a, contracts.ModeVideo), nil)
	code(t, err, contracts.InvalidSelection)
}
func TestPublicHostRedirectToPrivateRejected(t *testing.T) {
	// No network extraction is started, so a malicious redirect/CDN address
	// cannot reach a request executor. URL prevalidation alone is not the gate.
	r := &neverRunner{}
	e, _ := New(Config{HostSessionID: "session", Runner: r})
	_, err := e.Analyze(context.Background(), "https://vm.tiktok.com/owned-short-form/")
	code(t, err, contracts.NetworkPolicyBlocked)
	if r.calls != 0 {
		t.Fatal("extractor could perform an unchecked redirect")
	}
}
func TestMetadataAccessRestriction(t *testing.T) {
	for _, raw := range []string{`{"id":"restricted","has_drm":true}`, `{"id":"restricted","availability":"needs_auth"}`, `{"id":"restricted","availability":"premium_only"}`} {
		_, err := ParseMetadata([]byte(raw))
		code(t, err, contracts.AccessRestricted)
	}
}

type failureRunner struct{ next ports.ProcessRunner }

func (r failureRunner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	if s.Executable == ports.FFmpeg && strings.Contains(strings.Join(s.Args, " "), "-progress") {
		if emit != nil {
			emit(ports.ProcessLine{Stream: ports.Stderr, Text: "No space left on device; secret signed URL must stay private"})
		}
		return ports.ProcessResult{ExitCode: 1}, nil
	}
	return r.next.Run(ctx, s, emit)
}
func TestMediaDiskFullMappedWithoutRawLog(t *testing.T) {
	e, r, a := fixtureEngine(t)
	e.config.Runner = failureRunner{next: r}
	plan := localPlan(t, a, contracts.ModeMP3)
	plan.VideoChoice = nil
	_, err := e.Execute(context.Background(), plan, nil)
	code(t, err, contracts.DiskFull)
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("raw process log disclosed")
	}
	for _, dir := range []string{plan.WorkDir, plan.DestinationPath} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatalf("failure left files in %s: %v", dir, entries)
		}
	}
}

type uncertainTerminationRunner struct {
	cancel context.CancelFunc
	next   ports.ProcessRunner
}

func (r uncertainTerminationRunner) Run(ctx context.Context, s ports.ExecSpec, emit func(ports.ProcessLine)) (ports.ProcessResult, error) {
	if s.Executable == ports.FFmpeg && strings.Contains(strings.Join(s.Args, " "), "-progress") {
		if err := os.WriteFile(s.Args[len(s.Args)-1], []byte("active owned partial"), 0600); err != nil {
			return ports.ProcessResult{}, err
		}
		r.cancel()
		return ports.ProcessResult{}, &contracts.ProtocolError{Code: contracts.CancelFailed, Message: "Owned tree termination was not confirmed."}
	}
	return r.next.Run(ctx, s, emit)
}
func TestUnconfirmedTerminationKeepsOwnedIntermediate(t *testing.T) {
	e, r, a := fixtureEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.config.Runner = uncertainTerminationRunner{cancel: cancel, next: r}
	plan := localPlan(t, a, contracts.ModeMP3)
	_, err := e.Execute(ctx, plan, nil)
	code(t, err, contracts.CancelFailed)
	work, _ := os.ReadDir(plan.WorkDir)
	if len(work) != 1 {
		t.Fatalf("unconfirmed active job was cleaned: %v", work)
	}
	data, err := os.ReadFile(filepath.Join(plan.WorkDir, work[0].Name(), "processed.mp3"))
	if err != nil || string(data) != "active owned partial" {
		t.Fatal("active owned intermediate was removed")
	}
	destination, _ := os.ReadDir(plan.DestinationPath)
	if len(destination) != 0 {
		t.Fatalf("uncertain termination published a final: %v", destination)
	}
}
func TestFinalizeCanceledDoesNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := testutil.TempDir(t)
	source := filepath.Join(testutil.TempDir(t), "source")
	_ = os.WriteFile(source, []byte("bytes"), 0600)
	_, err := finalize(ctx, source, dir, "Media", "mp4", "job", contracts.CompletedFile{}, nil)
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("canceled task published %v", entries)
	}
}
func TestFinalizeRequiresValidIntermediateJobIdentity(t *testing.T) {
	destination := testutil.TempDir(t)
	source := filepath.Join(testutil.TempDir(t), "source")
	if err := os.WriteFile(source, []byte("owned media"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := finalize(context.Background(), source, destination, "Media", "mp4", "../outside", contracts.CompletedFile{}, nil)
	code(t, err, contracts.DestinationError)
	files, _ := os.ReadDir(destination)
	if len(files) != 0 {
		t.Fatalf("invalid job created files: %v", files)
	}
}

type cancelOnIntermediateContext struct {
	context.Context
	destination string
	knownName   string
	cancel      context.CancelFunc
}

func (c cancelOnIntermediateContext) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	files, _ := os.ReadDir(c.destination)
	for _, file := range files {
		if file.Name() != c.knownName {
			c.cancel()
			return context.Canceled
		}
	}
	return nil
}
func TestFinalizeCancellationCleansOwnedIntermediateOnly(t *testing.T) {
	destination := testutil.TempDir(t)
	known := ".dwnl-unrelated.partial"
	knownPath := filepath.Join(destination, known)
	if err := os.WriteFile(knownPath, []byte("unrelated user data"), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(testutil.TempDir(t), "source")
	if err := os.WriteFile(source, []byte("owned media"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := finalize(cancelOnIntermediateContext{Context: ctx, destination: destination, knownName: known, cancel: cancel}, source, destination, "Media", "mp4", "cleanup-job", contracts.CompletedFile{}, nil)
	if err != context.Canceled {
		t.Fatalf("cancellation after intermediate creation: %v", err)
	}
	files, _ := os.ReadDir(destination)
	if len(files) != 1 || files[0].Name() != known {
		t.Fatalf("owned cleanup affected unexpected files: %v", files)
	}
	data, err := os.ReadFile(knownPath)
	if err != nil || string(data) != "unrelated user data" {
		t.Fatal("unrelated destination file changed")
	}
}

// MP4 is the only video output: Opus audio becomes AAC, VP9 is copied and
// VP8, which MP4 cannot carry, is re-encoded to H.264.
func TestIncompatibleStreamsAreConvertedIntoMP4(t *testing.T) {
	for _, tc := range []struct{ encoder, want string }{{"libvpx-vp9", "vp9"}, {"libvpx", "h264"}} {
		t.Run(tc.encoder, func(t *testing.T) {
			r := mediaRunner(t)
			dir := testutil.TempDir(t)
			video, audio := filepath.Join(dir, "video.webm"), filepath.Join(dir, "audio.webm")
			for _, a := range [][]string{
				{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:size=160x90:rate=25", "-t", "2", "-c:v", tc.encoder, "-an", video},
				{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2", "-c:a", "libopus", audio},
			} {
				if out, err := exec.Command(filepath.Join(r.bin, ports.ExecutableFile(ports.FFmpeg)), a...).CombinedOutput(); err != nil {
					t.Fatalf("fixture generation: %v %s", err, out)
				}
			}
			e, err := New(Config{HostSessionID: "local-session", Runner: r})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Probe(context.Background()); err != nil {
				t.Fatal(err)
			}
			a, err := e.ImportLocal(context.Background(), fixtureURL, LocalMedia{VideoPath: video, AudioPath: audio, Title: "Conversion fixture"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(context.Background(), localPlan(t, a, contracts.ModeVideo), nil)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(result.Path) != ".mp4" || result.Format != "mp4" || result.VideoCodec != tc.want || result.AudioCodec != "aac" {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
