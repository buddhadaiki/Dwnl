package engine

import (
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"path/filepath"
	"slices"
	"strings"
)

const localFormats = "mov,matroska,webm,wav,mp3"

var ytdlpVersionArgs = []string{"--ignore-config", "--no-config-locations", "--no-plugin-dirs", "--no-remote-components", "--version"}

// Conversions Execute requests when a stream does not fit in MP4 as-is.
var (
	aacArgs  = []string{"-c:v", "copy", "-c:a", "aac", "-b:a", "192k"}
	h264Args = []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "192k"}
)

// ValidateExecSpec is defense in depth for the engine's closed subprocess
// grammar. Browser metadata never supplies flags, executable paths, or inputs.
func ValidateExecSpec(s ports.ExecSpec) error {
	bad := func() error {
		return protocolError(contracts.InvalidSelection, "The media command is outside the allowed operation.")
	}
	a := s.Args
	switch s.Executable {
	case ports.YTDLP:
		if slices.Equal(a, ytdlpVersionArgs) {
			return nil
		}
		if validateOnlineSpec(s) {
			return nil
		}
		return bad()
	case ports.Deno:
		if slices.Equal(a, []string{"--version"}) {
			return nil
		}
		return bad()
	case ports.FFmpeg:
		if slices.Equal(a, []string{"-version"}) || slices.Equal(a, []string{"-hide_banner", "-encoders"}) {
			return nil
		}
	case ports.FFprobe:
		if slices.Equal(a, []string{"-version"}) {
			return nil
		}
		prefix := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", localFormats, "-show_format", "-show_streams", "-of", "json"}
		if len(a) != len(prefix)+1 || !slices.Equal(a[:len(prefix)], prefix) || !safeInput(a[len(prefix)]) {
			return bad()
		}
		return nil
	default:
		return bad()
	}
	prefix := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-n"}
	if len(a) < len(prefix) || !slices.Equal(a[:len(prefix)], prefix) || !filepath.IsAbs(s.Dir) {
		return bad()
	}
	a = a[len(prefix):]
	inputs := 0
	for len(a) >= 6 && a[0] == "-protocol_whitelist" {
		if !slices.Equal(a[:5], []string{"-protocol_whitelist", "file,pipe", "-format_whitelist", localFormats, "-i"}) || !safeInput(a[5]) {
			return bad()
		}
		inputs++
		if inputs > 2 {
			return bad()
		}
		a = a[6:]
	}
	if inputs == 0 {
		return bad()
	}
	maps, audio := 0, false
	for len(a) >= 2 && a[0] == "-map" {
		if a[1] != "0:v:0" && a[1] != "0:a:0" && !(inputs == 2 && a[1] == "1:a:0") {
			return bad()
		}
		audio = audio || a[1] != "0:v:0"
		maps++
		a = a[2:]
		if maps > 2 {
			return bad()
		}
	}
	if maps == 0 {
		return bad()
	}
	encodesMP4 := false
	if len(a) >= 2 && slices.Equal(a[:2], []string{"-c", "copy"}) {
		a = a[2:]
	} else if len(a) >= len(aacArgs) && slices.Equal(a[:len(aacArgs)], aacArgs) && audio {
		a, encodesMP4 = a[len(aacArgs):], true
	} else if len(a) >= len(h264Args) && slices.Equal(a[:len(h264Args)], h264Args) {
		a, encodesMP4 = a[len(h264Args):], true
	} else {
		if len(a) < 9 || !slices.Equal(a[:4], []string{"-vn", "-c:a", "libmp3lame", "-b:a"}) || (a[4] != "128k" && a[4] != "192k" && a[4] != "320k") || !slices.Equal(a[5:9], []string{"-ar", "44100", "-write_xing", "0"}) {
			return bad()
		}
		a = a[9:]
	}
	tail := []string{"-map_metadata", "-1", "-progress", "pipe:1", "-nostats"}
	if len(a) != len(tail)+1 || !slices.Equal(a[:len(tail)], tail) {
		return bad()
	}
	output := a[len(tail)]
	ext := strings.ToLower(filepath.Ext(output))
	if !filepath.IsAbs(output) || filepath.Clean(filepath.Dir(output)) != filepath.Clean(s.Dir) || !slices.Contains([]string{".mp4", ".webm", ".mkv", ".mp3"}, ext) || encodesMP4 && ext != ".mp4" {
		return bad()
	}
	return nil
}
func safeInput(path string) bool {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	return validateLocalPath(path, false) == nil
}
