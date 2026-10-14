package engine

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"errors"
	"os"
	"testing"
)

type neverRunner struct{ calls int }

func (r *neverRunner) Run(context.Context, ports.ExecSpec, func(ports.ProcessLine)) (ports.ProcessResult, error) {
	r.calls++
	return ports.ProcessResult{}, errors.New("unexpected subprocess")
}
func code(t *testing.T, err error, want contracts.ErrorCode) {
	t.Helper()
	var p *contracts.ProtocolError
	if !errors.As(err, &p) || p.Code != want {
		t.Fatalf("error=%v; want %s", err, want)
	}
}
func TestNetworkAnalysisBlockedBeforeAnySubprocess(t *testing.T) {
	for _, raw := range []string{"https://youtu.be/owned-fixture", "https://www.instagram.com/reel/owned-fixture/", "https://www.tiktok.com/@owned-fixture/video/123", "https://x.com/owned-fixture/status/123"} {
		t.Run(raw, func(t *testing.T) {
			r := &neverRunner{}
			e, _ := New(Config{HostSessionID: "session", Runner: r})
			_, err := e.Analyze(context.Background(), raw)
			code(t, err, contracts.NetworkPolicyBlocked)
			if r.calls != 0 || e.Capabilities().NetworkAccess {
				t.Fatal("blocked analysis invoked a process or advertised network")
			}
			t.Logf("error=%v; subprocessCalls=%d; networkAccess=%t; no site request attempted", err, r.calls, e.Capabilities().NetworkAccess)
		})
	}
}
func TestCanonicalSourceURLExactHosts(t *testing.T) {
	for _, raw := range []string{"http://youtube.com/watch?v=a", "https://youtube.com.evil.test/watch?v=a", "https://user:pass@youtube.com/watch?v=a", "https://127.0.0.1/", "https://www.youtube.com:444/watch?v=a", "https://www.youtube.com./watch?v=a", "https://www.youtube.com\\@evil.test/"} {
		if _, err := CanonicalSourceURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	got, err := CanonicalSourceURL("https://WWW.YouTube.com/watch?v=owned-fixture&utm_source=tracker&token=secret#discard")
	if err != nil || got != "https://www.youtube.com/watch?v=owned-fixture" {
		t.Fatalf("canonical=%q err=%v", got, err)
	}
	for _, raw := range []string{"https://youtu.be/abc", "https://www.instagram.com/reel/abc/", "https://vm.tiktok.com/abc/", "https://x.com/user/status/123"} {
		if _, err := CanonicalSourceURL(raw); err != nil {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
}
func TestParseAvailableFormats(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/ytdlp-formats.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := ParseMetadata(raw)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	first := items[0]
	if !first.HasAudio || len(first.VideoChoices) != 2 {
		t.Fatalf("first=%+v", first)
	}
	if first.VideoChoices[0].Container != "mp4" || !first.VideoChoices[0].HasAudio || first.VideoChoices[0].AudioCodec != "mp4a.40.2" || first.VideoChoices[0].Height == nil || *first.VideoChoices[0].Height != 1080 {
		t.Fatalf("1080 choice=%+v", first.VideoChoices[0])
	}
	if first.VideoChoices[1].Height != nil {
		t.Fatal("invented missing height")
	}
	if items[1].HasAudio || items[1].VideoChoices[0].HasAudio {
		t.Fatal("invented silent-item audio")
	}
}
func TestKnownExecutableAndArgsOnly(t *testing.T) {
	for _, s := range []ports.ExecSpec{{Executable: "powershell", Args: []string{"-Command", "whoami"}}, {Executable: ports.FFmpeg, Args: []string{"-i", "https://public.example/a"}}, {Executable: ports.YTDLP, Args: []string{"--exec", "arbitrary"}}, {Executable: ports.FFprobe, Args: []string{"-i", "file:secret"}}} {
		if ValidateExecSpec(s) == nil {
			t.Errorf("accepted %+v", s)
		}
	}
	if err := ValidateExecSpec(ports.ExecSpec{Executable: ports.FFmpeg, Args: []string{"-version"}}); err != nil {
		t.Fatal(err)
	}
}
func TestWrongContainerRejected(t *testing.T) {
	var p mediaProbe
	p.Streams = []probeStream{{CodecType: "audio", CodecName: "mp3", Bitrate: "192000"}}
	p.Format.Duration = "2"
	p.Format.FormatName = "wav"
	code(t, validateMediaOutput(p, nil, 192, 2), contracts.InvalidOutput)
}
func TestWrongVideoDimensionsRejected(t *testing.T) {
	var p mediaProbe
	p.Streams = []probeStream{{CodecType: "video", CodecName: "h264", Width: 160, Height: 360}}
	p.Format.Duration = "2"
	p.Format.FormatName = "mov,mp4,m4a,3gp,3g2,mj2"
	height := uint64(90)
	code(t, validateMediaOutput(p, &contracts.VideoChoice{Container: "mp4", VideoCodec: "h264", Height: &height}, 0, 2), contracts.InvalidOutput)
}

type cancelFailureRunner struct{ cancel context.CancelFunc }

func (r cancelFailureRunner) Run(context.Context, ports.ExecSpec, func(ports.ProcessLine)) (ports.ProcessResult, error) {
	r.cancel()
	return ports.ProcessResult{}, &contracts.ProtocolError{Code: contracts.CancelFailed, Message: "Owned process termination was not confirmed."}
}
func TestTerminationFailureIsNotConvertedToCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e, _ := New(Config{HostSessionID: "session", Runner: cancelFailureRunner{cancel: cancel}})
	err := e.run(ctx, ports.ExecSpec{Executable: ports.FFmpeg, Args: []string{"-version"}}, nil)
	code(t, err, contracts.CancelFailed)
}

type nonzeroExitRunner struct{}

func (nonzeroExitRunner) Run(context.Context, ports.ExecSpec, func(ports.ProcessLine)) (ports.ProcessResult, error) {
	return ports.ProcessResult{ExitCode: 2}, errors.New("process exited with status 2")
}
func TestNonzeroProcessExitIsEngineError(t *testing.T) {
	e, _ := New(Config{HostSessionID: "session", Runner: nonzeroExitRunner{}})
	code(t, e.run(context.Background(), ports.ExecSpec{Executable: ports.FFmpeg, Args: []string{"-version"}}, nil), contracts.EngineError)
}
