package engine

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed transport/yt_dlp_plugins/extractor/dwnl.py
var transportFiles embed.FS
var formatIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type remotePick struct{ VideoID, AudioID string }
type remoteItem struct {
	ID      string
	AudioID string
	Choices map[string]remotePick
	Record  extractedItem
}
type onlineAnalysis struct{ Items map[string]remoteItem }

var onlineStaticArgs = []string{"--ignore-config", "--no-config-locations", "--no-plugin-dirs", "--no-remote-components", "--no-cookies", "--no-cookies-from-browser", "--no-cache-dir", "--no-geo-bypass", "--proxy", "", "--no-js-runtimes", "--js-runtimes"}

func onlineArgs(dir, bin string) []string {
	a := append([]string{}, onlineStaticArgs...)
	a = append(a, "deno:"+filepath.Join(bin, "deno.exe"), "--plugin-dirs", filepath.Join(dir, "plugins"), "--use-extractors", "^Dwnl$", "--extractor-args", "twitter:api=syndication", "--extractor-args", "youtube:player_client=visionos,web;fetch_pot=never", "--downloader", "native", "--fixup", "never", "--no-playlist", "--socket-timeout", "15", "--retries", "1", "--fragment-retries", "1", "--concurrent-fragments", "1", "--max-filesize", "5G", "--newline", "--progress-template", "download:DWNL_PROGRESS %(progress.downloaded_bytes)s %(progress.total_bytes)s")
	return a
}

func validateOnlineSpec(s ports.ExecSpec) bool {
	a := s.Args
	if !filepath.IsAbs(s.Dir) || len(a) <= len(onlineStaticArgs) || !slices.Equal(a[:len(onlineStaticArgs)], onlineStaticArgs) {
		return false
	}
	deno := strings.TrimPrefix(a[len(onlineStaticArgs)], "deno:")
	if !strings.HasPrefix(a[len(onlineStaticArgs)], "deno:") || !filepath.IsAbs(deno) || filepath.Base(deno) != "deno.exe" {
		return false
	}
	base := onlineArgs(s.Dir, filepath.Dir(deno))
	if len(a) < len(base) || !slices.Equal(a[:len(base)], base) {
		return false
	}
	tail := a[len(base):]
	if slices.Equal(tail, []string{"--list-extractors"}) {
		return true
	}
	if len(tail) < 4 || tail[0] != "--extractor-args" {
		return false
	}
	mode := tail[1]
	tail = tail[2:]
	if mode == "dwnl:mode=analyze" || mode == "dwnl:mode=probe" {
		if len(tail) != 3 || tail[0] != "--skip-download" || tail[1] != "--" {
			return false
		}
	} else if strings.HasPrefix(mode, "dwnl:mode=download;item=") {
		item := strings.TrimPrefix(mode, "dwnl:mode=download;item=")
		if !formatIDPattern.MatchString(item) || len(tail) != 6 || tail[0] != "--format" || !formatIDPattern.MatchString(tail[1]) || tail[2] != "--output" || tail[3] != "downloaded.bin" || tail[4] != "--" {
			return false
		}
	} else {
		return false
	}
	raw := tail[len(tail)-1]
	if mode == "dwnl:mode=probe" && raw != "https://www.youtube.com/watch?v=dwnl-private-self-test" {
		return false
	}
	canonical, err := CanonicalSourceURL(raw)
	return err == nil && canonical == raw
}

func onlineEnvironment(dir, bin string) []string {
	values := []string{"PATH=" + bin + string(os.PathListSeparator) + filepath.Join(os.Getenv("SYSTEMROOT"), "System32"), "TEMP=" + dir, "TMP=" + dir, "DENO_DIR=" + filepath.Join(dir, "deno-cache"), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1", "PYTHONDONTWRITEBYTECODE=1"}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		if value := os.Getenv(key); value != "" {
			values = append(values, key+"="+value)
		}
	}
	return values
}

func (e *Engine) onlineWork(parent string) (string, func(), error) {
	if err := ensureEngineRoot(parent); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(parent, "extractor-")
	if err != nil {
		return "", nil, fileError(err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", nil, err
	}
	owner := newID()
	marker := filepath.Join(dir, ".dwnl-engine-work")
	if err = os.WriteFile(marker, []byte(owner), 0600); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		current, err := os.Lstat(dir)
		if err == nil && os.SameFile(info, current) && !hasReparsePoint(dir) {
			_ = removeEngineWork(dir, owner)
		}
	}
	target := filepath.Join(dir, "plugins", "dwnl", "yt_dlp_plugins", "extractor", "dwnl.py")
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		cleanup()
		return "", nil, fileError(err)
	}
	body, err := transportFiles.ReadFile("transport/yt_dlp_plugins/extractor/dwnl.py")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err = os.WriteFile(target, body, 0600); err != nil {
		cleanup()
		return "", nil, fileError(err)
	}
	return dir, cleanup, nil
}

func (e *Engine) extractorRun(ctx context.Context, dir, mode, source, format string, emit func(contracts.Progress)) error {
	args := onlineArgs(dir, e.config.PrivateBinDir)
	args = append(args, "--extractor-args", mode)
	if format == "" {
		args = append(args, "--skip-download")
	} else {
		args = append(args, "--format", format, "--output", "downloaded.bin")
	}
	args = append(args, "--", source)
	var failure contracts.ErrorCode
	err := e.run(ctx, ports.ExecSpec{Executable: ports.YTDLP, Args: args, Dir: dir, Env: onlineEnvironment(dir, e.config.PrivateBinDir)}, func(line ports.ProcessLine) {
		value := line.Text
		if strings.Contains(value, "DWNL_NETWORK_POLICY:") {
			failure = contracts.NetworkPolicyBlocked
		}
		if strings.Contains(value, "DWNL_NETWORK_ERROR:") {
			failure = contracts.NetworkError
		}
		if strings.Contains(value, "DWNL_ACCESS_RESTRICTED:") || strings.Contains(value, "Sign in") || strings.Contains(value, "login required") {
			failure = contracts.AccessRestricted
		}
		if strings.Contains(value, "DWNL_INVALID_SELECTION:") || strings.Contains(value, "Requested format is not available") {
			failure = contracts.InvalidSelection
		}
		if e.config.NetworkTrace != nil && (strings.HasPrefix(value, "DWNL_NET ") || strings.Contains(value, "DWNL_")) {
			e.config.NetworkTrace(value)
		}
		if emit != nil && strings.HasPrefix(value, "DWNL_PROGRESS ") {
			p := contracts.Progress{Stage: contracts.StateDownloading}
			fields := strings.Fields(value)
			if len(fields) == 3 {
				if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil && n <= contracts.MaxSafeInteger {
					p.CompletedBytes = &n
				}
				if n, err := strconv.ParseUint(fields[2], 10, 64); err == nil && n > 0 && n <= contracts.MaxSafeInteger {
					p.TotalBytes = &n
				}
			}
			emit(p)
		}
	})
	if err == nil {
		return nil
	}
	var typed *contracts.ProtocolError
	if errors.As(err, &typed) && typed.Code == contracts.CancelFailed {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if failure != "" {
		return protocolError(failure, "The public source could not be accessed within the supported policy.")
	}
	return err
}

func readExtractorMetadata(dir string) ([]byte, error) {
	path := filepath.Join(dir, "metadata.json")
	if err := validateLocalPath(path, false); err != nil {
		return nil, protocolError(contracts.EngineError, "The extractor did not produce private metadata.")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 2*1024*1024 {
		return nil, protocolError(contracts.EngineError, "Extractor metadata exceeded its limit.")
	}
	return raw, nil
}

func (e *Engine) fetchOnlineMetadata(ctx context.Context, source, parent string) ([]byte, error) {
	dir, cleanup, err := e.onlineWork(parent)
	if err != nil {
		return nil, err
	}
	cleanupSafe := true
	defer func() {
		if cleanupSafe {
			cleanup()
		}
	}()
	limited, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err = e.extractorRun(limited, dir, "dwnl:mode=analyze", source, "", nil); err != nil {
		cleanupSafe = !isCancelFailure(err)
		return nil, err
	}
	return readExtractorMetadata(dir)
}

func audioFormat(formats []extractedFormat) string {
	best := ""
	rate := -1.0
	for _, audioOnly := range []bool{true, false} {
		for _, f := range formats {
			bitrate := f.ABR
			if bitrate <= 0 && !hasCodec(f.VCodec) {
				bitrate = f.TBR
			}
			if !f.DRM && hasCodec(f.ACodec) && hasCodec(f.VCodec) != audioOnly && bitrate > rate {
				best = f.ID
				rate = bitrate
			}
		}
		if best != "" {
			break
		}
	}
	return best
}

func (e *Engine) analyzeOnline(ctx context.Context, raw string) (contracts.Analysis, error) {
	canonical, err := CanonicalSourceURL(raw)
	if err != nil {
		return contracts.Analysis{}, err
	}
	if !filepath.IsAbs(e.config.PrivateBinDir) || !filepath.IsAbs(e.config.AnalysisRoot) {
		return contracts.Analysis{}, protocolError(contracts.NetworkPolicyBlocked, "The private extractor adapter is not configured in this host.")
	}
	data, err := e.fetchOnlineMetadata(ctx, canonical, e.config.AnalysisRoot)
	if err != nil {
		return contracts.Analysis{}, err
	}
	items, err := ParseMetadata(data)
	if err != nil {
		return contracts.Analysis{}, err
	}
	var root extractedItem
	if err = json.Unmarshal(data, &root); err != nil {
		return contracts.Analysis{}, err
	}
	records := []extractedItem{root}
	if root.Entries != nil {
		records = nil
		for _, entry := range root.Entries {
			var item extractedItem
			if err = json.Unmarshal(entry, &item); err != nil {
				return contracts.Analysis{}, err
			}
			records = append(records, item)
		}
	}
	if len(records) != len(items) {
		return contracts.Analysis{}, protocolError(contracts.EngineError, "Extractor item cardinality is inconsistent.")
	}
	online := &onlineAnalysis{Items: map[string]remoteItem{}}
	for i, record := range records {
		if !formatIDPattern.MatchString(record.ID) {
			return contracts.Analysis{}, protocolError(contracts.EngineError, "Extractor item identity is unsupported.")
		}
		ids := map[string]bool{}
		for _, f := range record.Formats {
			if !f.DRM && (!formatIDPattern.MatchString(f.ID) || ids[f.ID]) {
				return contracts.Analysis{}, protocolError(contracts.EngineError, "Extractor format identities are ambiguous.")
			}
			ids[f.ID] = true
		}
		audio := audioFormat(record.Formats)
		item := &items[i]
		item.ItemID = newID()
		remote := remoteItem{ID: record.ID, AudioID: audio, Choices: map[string]remotePick{}, Record: record}
		index := 0
		for _, f := range record.Formats {
			if f.DRM || !hasCodec(f.VCodec) {
				continue
			}
			choice := &item.VideoChoices[index]
			index++
			choice.OptionID = newID()
			selectedAudio := audio
			if selectedAudio == "" && hasCodec(f.ACodec) {
				selectedAudio = f.ID
			}
			remote.Choices[choice.OptionID] = remotePick{VideoID: f.ID, AudioID: selectedAudio}
		}
		online.Items[item.ItemID] = remote
	}
	analysis := contracts.Analysis{AnalysisID: newID(), HostSessionID: e.config.HostSessionID, SourceURL: canonical, ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339), Items: items}
	return e.remember(localAnalysis{analysis: analysis, online: online}), nil
}

func isCancelFailure(err error) bool {
	var p *contracts.ProtocolError
	return errors.As(err, &p) && p.Code == contracts.CancelFailed
}
func probeCodec(codec string) string {
	for prefix, name := range map[string]string{"avc1": "h264", "avc3": "h264", "hev1": "hevc", "hvc1": "hevc", "av01": "av1", "vp09": "vp9", "mp4a": "aac", "bytevc1": "hevc", "h265": "hevc"} {
		if strings.HasPrefix(codec, prefix) {
			return name
		}
	}
	return codec
}
func selectedFormat(record extractedItem, id string) (extractedFormat, bool) {
	for _, f := range record.Formats {
		if f.ID == id && !f.DRM {
			return f, true
		}
	}
	return extractedFormat{}, false
}
func (e *Engine) downloadOnline(ctx context.Context, stored localAnalysis, choice *contracts.VideoChoice, plan contracts.DownloadPlan, emit func(contracts.Progress)) (localAnalysis, func(), error) {
	remote, ok := stored.online.Items[stored.analysis.Items[0].ItemID]
	if !ok {
		return stored, nil, protocolError(contracts.InvalidSelection, "The private selection is unavailable.")
	}
	ids := []string{remote.AudioID}
	if choice != nil {
		pick, ok := remote.Choices[choice.OptionID]
		if !ok {
			return stored, nil, protocolError(contracts.InvalidSelection, "The private video format is unavailable.")
		}
		ids = []string{pick.VideoID}
		if (choice.HasAudio || choice.AudioUnknown) && pick.AudioID != pick.VideoID {
			ids = append(ids, pick.AudioID)
		}
	}
	var cleanups []func()
	cleanup := func() {
		for _, f := range cleanups {
			f()
		}
	}
	var total int64
	for index, id := range ids {
		format, ok := selectedFormat(remote.Record, id)
		if !ok {
			cleanup()
			return stored, nil, protocolError(contracts.InvalidSelection, "The selected source format is unavailable.")
		}
		dir, remove, err := e.onlineWork(plan.WorkDir)
		if err != nil {
			cleanup()
			return stored, nil, err
		}
		guard := map[string]any{"id": remote.ID, "duration": remote.Record.Duration, "format": format, "budget_bytes": 5*1024*1024*1024 - total}
		if e.config.NetworkTrace != nil {
			rate := format.ABR
			if rate <= 0 && !hasCodec(format.VCodec) {
				rate = format.TBR
			}
			label := "unknown"
			if rate > 0 {
				label = strconv.FormatFloat(rate, 'f', -1, 64)
			}
			e.config.NetworkTrace("DWNL_SELECTION format=" + id + " reported_audio_kbps=" + label)
		}
		body, err := json.Marshal(guard)
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "selection.json"), body, 0600)
		}
		if err == nil {
			err = e.extractorRun(ctx, dir, "dwnl:mode=download;item="+remote.ID, stored.analysis.SourceURL, id, emit)
		}
		if isCancelFailure(err) {
			return stored, nil, err
		} // Keep all work until owned process termination is known.
		cleanups = append(cleanups, remove)
		if err != nil {
			cleanup()
			return stored, nil, err
		}
		// Native adapter validates the selection before requests for media bytes;
		// verify its bounded report again before local processing.
		data, err := readExtractorMetadata(dir)
		var root extractedItem
		if err == nil {
			err = json.Unmarshal(data, &root)
		}
		matching := false
		for _, entry := range root.Entries {
			var current extractedItem
			if json.Unmarshal(entry, &current) != nil {
				continue
			}
			if current.ID != remote.ID {
				continue
			}
			actual, found := selectedFormat(current, id)
			matching = found && sameFormat(format, actual)
		}
		if err != nil || !matching {
			cleanup()
			return stored, nil, protocolError(contracts.InvalidSelection, "The public source format changed. Analyze it again.")
		}
		path := filepath.Join(dir, "downloaded.bin")
		proof, err := proveSource(ctx, path)
		if err != nil {
			cleanup()
			return stored, nil, protocolError(contracts.InvalidOutput, "The downloaded media is not a safe local file.")
		}
		total += proof.info.Size()
		if total <= 0 || total > 5*1024*1024*1024 {
			cleanup()
			return stored, nil, protocolError(contracts.InvalidOutput, "The media exceeds the five GiB job limit.")
		}
		p, err := e.probeMedia(ctx, path)
		if isCancelFailure(err) {
			return stored, nil, err
		}
		if err != nil {
			cleanup()
			return stored, nil, err
		}
		if p.duration() == 0 || p.duration() > 21600 || (remote.Record.Duration > 0 && absDuration(p.duration()-remote.Record.Duration) > max(.5, remote.Record.Duration*.01)) {
			cleanup()
			return stored, nil, protocolError(contracts.InvalidOutput, "The downloaded duration failed validation.")
		}
		v, a := p.stream("video"), p.stream("audio")
		if hasCodec(format.VCodec) != (v != nil) || format.ACodec != "unknown" && hasCodec(format.ACodec) != (a != nil) || (v != nil && (format.VCodec != "unknown" && v.CodecName != probeCodec(format.VCodec) || format.Width > 0 && v.Width != uint64(format.Width) || format.Height > 0 && v.Height != uint64(format.Height))) || a != nil && format.ACodec != "unknown" && a.CodecName != probeCodec(format.ACodec) {
			cleanup()
			return stored, nil, protocolError(contracts.InvalidOutput, "The downloaded streams differ from the selected source format.")
		}
		stored.proofs = append(stored.proofs, proof)
		if choice != nil && index == 0 {
			stored.media.VideoPath = path
			stored.probe = p
			if choice.VideoCodec == "unknown" {
				choice.VideoCodec = v.CodecName
			}
			if choice.AudioUnknown && len(ids) == 1 {
				choice.HasAudio = a != nil
				choice.AudioUnknown = false
				if a != nil {
					choice.AudioCodec = a.CodecName
				}
			}
		} else {
			if a == nil {
				cleanup()
				return stored, nil, protocolError(contracts.NoAudio, "The downloaded source has no audio stream.")
			}
			stored.media.AudioPath = path
			stored.audio = p
			if choice != nil && choice.AudioUnknown {
				choice.HasAudio = true
				choice.AudioUnknown = false
				choice.AudioCodec = a.CodecName
			}
		}
	}
	return stored, cleanup, nil
}
func absDuration(n float64) float64 {
	if n < 0 {
		return -n
	}
	return n
}
func sameFormat(a, b extractedFormat) bool {
	return a.ID == b.ID && a.VCodec == b.VCodec && a.ACodec == b.ACodec && reflect.DeepEqual(positiveUint(a.Width), positiveUint(b.Width)) && reflect.DeepEqual(positiveUint(a.Height), positiveUint(b.Height))
}
