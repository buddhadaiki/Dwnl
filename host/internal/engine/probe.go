package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
)

// probeRules names the checks below. Change it whenever Probe learns something
// new, so a result saved by an older build is never reused.
const probeRules = "dwnl-probe-3"

const (
	adapterProbeURL    = "https://www.youtube.com/watch?v=dwnl-private-self-test"
	adapterReadyMarker = "DWNL_ADAPTER_READY"
)

type versionCheck struct {
	version string
	err     error
}

func (c versionCheck) dependency(role ports.ExecutableRole) contracts.Dependency {
	if c.err != nil {
		return contracts.Dependency{Name: string(role), Version: "unavailable"}
	}
	return contracts.Dependency{Name: string(role), Version: c.version, Available: true}
}

// Probe reads versions and encoder capabilities without any media URL. A
// missing dependency is represented in handshake capabilities, not hidden.
//
// yt-dlp is a packed executable that unpacks itself on every start, about 3.5 s
// on the development machine, while each Deno or FFmpeg check takes about
// 0.1 s. So yt-dlp starts once, for the adapter self-test that also reports its
// version, and the other checks run during that start.
func (e *Engine) Probe(ctx context.Context) error {
	var ytdlp versionCheck
	var adapterReady bool
	var adapterErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		ytdlp, adapterReady, adapterErr = e.probeYTDLP(ctx)
	}()
	deno := e.checkVersion(ctx, ports.Deno)
	ffmpeg := e.checkVersion(ctx, ports.FFmpeg)
	ffprobe := e.checkVersion(ctx, ports.FFprobe)
	video := ffmpeg.err == nil && ffprobe.err == nil
	mp3 := false
	var encodersErr error
	if video {
		mp3, encodersErr = e.probeMP3(ctx)
	}
	<-done
	e.mu.Lock()
	e.dependencies = []contracts.Dependency{ytdlp.dependency(ports.YTDLP), deno.dependency(ports.Deno), ffmpeg.dependency(ports.FFmpeg), ffprobe.dependency(ports.FFprobe)}
	e.capabilities = contracts.Capabilities{Video: video, MP3: mp3, NetworkAccess: adapterReady && deno.err == nil}
	e.mu.Unlock()
	// The first failure in the order the checks once ran one after another.
	for _, err := range []error{ytdlp.err, deno.err, ffmpeg.err, ffprobe.err, encodersErr, adapterErr} {
		if err != nil {
			return err
		}
	}
	return nil
}

// probeYTDLP starts yt-dlp once when the private adapter can run: its ready
// line carries yt-dlp's version. Only when the self-test cannot run or fails
// does a plain version check tell a missing yt-dlp from a broken adapter.
func (e *Engine) probeYTDLP(ctx context.Context) (versionCheck, bool, error) {
	if !filepath.IsAbs(e.config.PrivateBinDir) || !filepath.IsAbs(e.config.AnalysisRoot) {
		return e.checkVersion(ctx, ports.YTDLP), false, nil
	}
	version, err := e.adapterSelfTest(ctx)
	if err != nil {
		return e.checkVersion(ctx, ports.YTDLP), false, err
	}
	return versionCheck{version: version}, true, nil
}

// adapterSelfTest loads the embedded adapter in yt-dlp without any network
// request and returns the yt-dlp version from the adapter's ready line.
func (e *Engine) adapterSelfTest(ctx context.Context) (string, error) {
	dir, cleanup, err := e.onlineWork(e.config.AnalysisRoot)
	if err != nil {
		return "", err
	}
	args := append(onlineArgs(dir, e.config.PrivateBinDir), "--extractor-args", "dwnl:mode=probe", "--skip-download", "--", adapterProbeURL)
	output, err := e.capture(ctx, ports.ExecSpec{Executable: ports.YTDLP, Args: args, Dir: dir, Env: onlineEnvironment(dir, e.config.PrivateBinDir)})
	if !isCancelFailure(err) {
		cleanup()
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		if _, version, found := strings.Cut(line, adapterReadyMarker); found {
			return versionLine(version), nil
		}
	}
	return "", protocolError(contracts.EngineError, "The private adapter did not confirm it loaded.")
}

func (e *Engine) checkVersion(ctx context.Context, role ports.ExecutableRole) versionCheck {
	args := []string{"-version"}
	switch role {
	case ports.YTDLP:
		args = ytdlpVersionArgs
	case ports.Deno:
		args = []string{"--version"}
	}
	text, err := e.capture(ctx, ports.ExecSpec{Executable: role, Args: args})
	if err != nil {
		return versionCheck{err: err}
	}
	return versionCheck{version: versionLine(text)}
}

// versionLine is the first line of a version report, bounded for display.
func versionLine(text string) string {
	version := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if len(version) > 512 {
		version = version[:512]
	}
	if version == "" {
		version = "unknown"
	}
	return version
}

func (e *Engine) probeMP3(ctx context.Context) (bool, error) {
	encoders, err := e.capture(ctx, ports.ExecSpec{Executable: ports.FFmpeg, Args: []string{"-hide_banner", "-encoders"}})
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(encoders, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == "libmp3lame" {
			return true, nil
		}
	}
	return false, nil
}

// ProbeIdentity names everything a Probe result depends on: these rules, the
// embedded adapter, the private folders and each executable's file identity.
// Installing, updating, replacing or removing a dependency changes it, so a
// host may reuse a saved result only while the identity still matches.
func (e *Engine) ProbeIdentity() (string, error) {
	if !filepath.IsAbs(e.config.PrivateBinDir) || !filepath.IsAbs(e.config.AnalysisRoot) {
		return "", errors.New("private executable and analysis folders are required")
	}
	adapter, err := transportFiles.ReadFile("transport/yt_dlp_plugins/extractor/dwnl.py")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(adapter)
	parts := []string{probeRules, hex.EncodeToString(sum[:]), filepath.Clean(e.config.PrivateBinDir), filepath.Clean(e.config.AnalysisRoot)}
	for _, role := range []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe} {
		identity, err := fileIdentity(filepath.Join(e.config.PrivateBinDir, ports.ExecutableFile(role)))
		if err != nil {
			return "", err
		}
		parts = append(parts, string(role)+"="+identity)
	}
	runtime, err := runtimeIdentity(filepath.Join(e.config.PrivateBinDir, "_internal"))
	if err != nil {
		return "", err
	}
	parts = append(parts, "yt-dlp-runtime="+runtime)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(digest[:]), nil
}

// runtimeIdentity names the onedir yt-dlp runtime folder beside the
// executable: every member's relative path, size and last write. A missing
// folder is a valid state of its own (the single-file build has none); links
// and other irregular members are refused.
func runtimeIdentity(root string) (string, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || linked(info) {
		return "", errors.New("yt-dlp runtime is not a plain folder")
	}
	hash := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		member, err := entry.Info()
		if err != nil {
			return err
		}
		if linked(member) || (!member.IsDir() && !member.Mode().IsRegular()) {
			return errors.New("yt-dlp runtime holds an irregular member")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s|%t|%d|%d\n", filepath.ToSlash(rel), member.IsDir(), member.Size(), member.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// linked reports links and, on Windows, junctions and other reparse points.
func linked(info fs.FileInfo) bool {
	return info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0
}

// RestoreProbe adopts the result an earlier Probe reported for the same
// ProbeIdentity, without starting any process.
func (e *Engine) RestoreProbe(capabilities contracts.Capabilities, dependencies []contracts.Dependency) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.capabilities = capabilities
	e.dependencies = append([]contracts.Dependency(nil), dependencies...)
}
