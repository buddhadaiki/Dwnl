package engine

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/storage"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type probeStream struct {
	Index     int    `json:"index"`
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     uint64 `json:"width"`
	Height    uint64 `json:"height"`
	Bitrate   string `json:"bit_rate"`
	StartTime string `json:"start_time"`
	Duration  string `json:"duration"`
}
type mediaProbe struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		Bitrate    string `json:"bit_rate"`
	} `json:"format"`
}
type fileProof struct {
	path string
	info os.FileInfo
	hash string
}

func proveSource(ctx context.Context, path string) (fileProof, error) {
	var empty fileProof
	if err := validateLocalPath(path, false); err != nil {
		return empty, err
	}
	f, err := os.Open(path)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return empty, err
	}
	digest := sha256.New()
	size, err := io.Copy(digest, contextReader{ctx: ctx, r: f})
	if err != nil {
		return empty, err
	}
	after, err := f.Stat()
	if err != nil {
		return empty, err
	}
	if !os.SameFile(before, after) || before.Size() != size || after.Size() != size || before.ModTime() != after.ModTime() {
		return empty, errors.New("source changed during analysis")
	}
	return fileProof{path: path, info: before, hash: hex.EncodeToString(digest.Sum(nil))}, nil
}
func validateSources(ctx context.Context, proofs []fileProof) error {
	for _, proof := range proofs {
		now, err := proveSource(ctx, proof.path)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return protocolError(contracts.InvalidSelection, "The local source changed. Analyze it again.")
		}
		if !os.SameFile(proof.info, now.info) || proof.hash != now.hash {
			return protocolError(contracts.InvalidSelection, "The local source changed. Analyze it again.")
		}
	}
	return nil
}

func validateLocalPath(path string, directory bool) error {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") || strings.HasPrefix(path, `\\`) {
		return errors.New("path must be an absolute local path")
	}
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	current := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(path, current)
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || hasReparsePoint(current) {
			return errors.New("reparse paths are not supported")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory && !info.IsDir() {
		return errors.New("path is not a directory")
	}
	if !directory && (!info.Mode().IsRegular() || info.Size() == 0) {
		return errors.New("path is not a nonempty regular file")
	}
	return nil
}
func (e *Engine) probeMedia(ctx context.Context, path string) (mediaProbe, error) {
	var p mediaProbe
	if err := validateLocalPath(path, false); err != nil {
		return p, protocolError(contracts.InvalidOutput, "The media file is missing or is not a safe local file.")
	}
	raw, err := e.capture(ctx, ports.ExecSpec{Executable: ports.FFprobe, Args: []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", localFormats, "-show_format", "-show_streams", "-of", "json", path}})
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil || len(p.Streams) == 0 {
		return p, protocolError(contracts.InvalidOutput, "The media file has no readable streams.")
	}
	return p, nil
}
func (p mediaProbe) stream(kind string) *probeStream {
	for _, s := range p.Streams {
		if s.CodecType == kind {
			copy := s
			return &copy
		}
	}
	return nil
}
func (p mediaProbe) duration() float64 {
	n, _ := strconv.ParseFloat(p.Format.Duration, 64)
	if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return n
}

// ImportLocal is a trusted host-tool/test API, never a browser operation. It
// imports authorized local fixtures; Analyze always keeps real sites blocked.
func (e *Engine) ImportLocal(ctx context.Context, sourceURL string, media LocalMedia) (contracts.Analysis, error) {
	canonical, err := CanonicalSourceURL(sourceURL)
	if err != nil {
		return contracts.Analysis{}, err
	}
	if media.VideoPath == "" && media.AudioPath == "" {
		return contracts.Analysis{}, protocolError(contracts.InvalidOutput, "No local media was selected.")
	}
	var proofs []fileProof
	for _, path := range []string{media.VideoPath, media.AudioPath} {
		if path == "" {
			continue
		}
		proof, err := proveSource(ctx, path)
		if err != nil {
			if ctx.Err() != nil {
				return contracts.Analysis{}, ctx.Err()
			}
			return contracts.Analysis{}, protocolError(contracts.InvalidOutput, "The media source is not a safe local file.")
		}
		proofs = append(proofs, proof)
	}
	var p, a mediaProbe
	if media.VideoPath != "" {
		p, err = e.probeMedia(ctx, media.VideoPath)
		if err != nil {
			return contracts.Analysis{}, err
		}
	}
	if media.AudioPath != "" {
		a, err = e.probeMedia(ctx, media.AudioPath)
		if err != nil {
			return contracts.Analysis{}, err
		}
		if a.stream("audio") == nil {
			return contracts.Analysis{}, protocolError(contracts.NoAudio, "The selected audio source has no decodable audio stream.")
		}
	}
	audio := a.stream("audio")
	if audio == nil {
		audio = p.stream("audio")
	}
	video := p.stream("video")
	title := media.Title
	if strings.TrimSpace(title) == "" {
		title = "Local media"
	}
	for len(title) > 4096 {
		title = string([]rune(title)[:len([]rune(title))-1])
	}
	item := contracts.MediaItem{ItemID: newID(), Title: title, HasAudio: audio != nil, VideoChoices: []contracts.VideoChoice{}}
	duration := p.duration()
	if duration == 0 {
		duration = a.duration()
	}
	if duration > 0 {
		item.DurationSeconds = &duration
	}
	if video != nil {
		acodec := ""
		if audio != nil {
			acodec = audio.CodecName
		}
		choice := contracts.VideoChoice{OptionID: newID(), Container: containerFor(video.CodecName, acodec), VideoCodec: video.CodecName, AudioCodec: acodec, HasAudio: audio != nil}
		if video.Width > 0 && video.Width <= contracts.MaxSafeInteger {
			width := video.Width
			choice.Width = &width
		}
		if video.Height > 0 && video.Height <= contracts.MaxSafeInteger {
			height := video.Height
			choice.Height = &height
		}
		item.VideoChoices = append(item.VideoChoices, choice)
	}
	if video == nil && audio == nil {
		return contracts.Analysis{}, protocolError(contracts.InvalidOutput, "The local file contains no audio or video stream.")
	}
	analysis := contracts.Analysis{AnalysisID: newID(), HostSessionID: e.config.HostSessionID, SourceURL: canonical, ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339), Items: []contracts.MediaItem{item}}
	if err := validateSources(ctx, proofs); err != nil {
		return contracts.Analysis{}, err
	}
	return e.remember(localAnalysis{analysis: analysis, media: media, probe: p, audio: a, proofs: proofs}), nil
}
func (e *Engine) selection(plan contracts.DownloadPlan) (localAnalysis, *contracts.VideoChoice, error) {
	var empty localAnalysis
	if plan.HostSessionID != e.config.HostSessionID || plan.Analysis.HostSessionID != e.config.HostSessionID {
		return empty, nil, protocolError(contracts.SessionEnded, "This selection belongs to a different host session.")
	}
	e.mu.Lock()
	stored, ok := e.analyses[plan.Analysis.AnalysisID]
	e.mu.Unlock()
	if !ok {
		return empty, nil, protocolError(contracts.AnalysisExpired, "Analyze this media again before saving it.")
	}
	expires, err := time.Parse(time.RFC3339, stored.analysis.ExpiresAt)
	if err != nil || !time.Now().Before(expires) {
		return empty, nil, protocolError(contracts.AnalysisExpired, "The media analysis expired. Analyze it again.")
	}
	item := stored.analysis.Items[0]
	if plan.Analysis.SourceURL != stored.analysis.SourceURL || plan.Item.ItemID != item.ItemID || plan.JobID == "" || plan.DestinationID == "" {
		return empty, nil, protocolError(contracts.InvalidSelection, "The selected item does not belong to this analysis.")
	}
	if plan.Mode == contracts.ModeMP3 {
		if plan.VideoChoice != nil || !(plan.MP3BitrateKbps == 128 || plan.MP3BitrateKbps == 192 || plan.MP3BitrateKbps == 320) {
			return empty, nil, protocolError(contracts.InvalidSelection, "Choose an available MP3 output rate.")
		}
		if !item.HasAudio {
			return empty, nil, protocolError(contracts.NoAudio, "The selected media has no source audio.")
		}
		if !e.Capabilities().MP3 {
			return empty, nil, protocolError(contracts.MP3Unavailable, "This FFmpeg build has no available MP3 encoder.")
		}
		return stored, nil, nil
	}
	if plan.Mode != contracts.ModeVideo || plan.VideoChoice == nil {
		return empty, nil, protocolError(contracts.InvalidSelection, "Choose an available video format.")
	}
	for _, choice := range item.VideoChoices {
		if choice.OptionID == plan.VideoChoice.OptionID && reflect.DeepEqual(choice, *plan.VideoChoice) {
			if !e.Capabilities().Video {
				return empty, nil, protocolError(contracts.DependencyUnavailable, "Local video processing is unavailable.")
			}
			return stored, &choice, nil
		}
	}
	return empty, nil, protocolError(contracts.InvalidSelection, "The selected video format does not belong to this analysis.")
}
func (e *Engine) Execute(ctx context.Context, plan contracts.DownloadPlan, emit func(contracts.Progress)) (contracts.CompletedFile, error) {
	var empty contracts.CompletedFile
	stored, choice, err := e.selection(plan)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := validateSources(ctx, stored.proofs); err != nil {
		return empty, err
	}
	if err := validateLocalPath(plan.WorkDir, true); err != nil {
		return empty, protocolError(contracts.DestinationError, "The private job directory is unavailable.")
	}
	if err := validateLocalPath(plan.DestinationPath, true); err != nil {
		return empty, protocolError(contracts.DestinationError, "The destination folder is unavailable or unsafe.")
	}
	if filepath.Clean(plan.WorkDir) == filepath.Clean(plan.DestinationPath) {
		return empty, protocolError(contracts.DestinationError, "The destination must be separate from the private job directory.")
	}
	private, err := os.MkdirTemp(plan.WorkDir, "media-")
	if err != nil {
		return empty, fileError(err)
	}
	privateInfo, err := os.Lstat(private)
	if err != nil {
		_ = os.Remove(private)
		return empty, fileError(err)
	}
	format := "mp3"
	stage := contracts.StateConverting
	if choice != nil {
		format = choice.Container
		stage = contracts.StateMuxing
	}
	output := filepath.Join(private, "processed."+format)
	cleanupSafe := true
	defer func() {
		if !cleanupSafe {
			return
		}
		info, err := os.Lstat(private)
		if err == nil && os.SameFile(info, privateInfo) && !hasReparsePoint(private) {
			if f, err := os.Lstat(output); err == nil && f.Mode().IsRegular() && !hasReparsePoint(output) {
				_ = os.Remove(output)
			}
			_ = os.Remove(private)
		}
	}()
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-n"}
	input := func(path string) {
		args = append(args, "-protocol_whitelist", "file,pipe", "-format_whitelist", localFormats, "-i", path)
	}
	expectedDuration := stored.probe.duration()
	if choice != nil {
		input(stored.media.VideoPath)
		args = append(args, "-map", "0:v:0")
		if choice.HasAudio {
			if stored.media.AudioPath != "" { // Input options precede all output mappings.
				args = args[:len(args)-2]
				input(stored.media.AudioPath)
				args = append(args, "-map", "0:v:0", "-map", "1:a:0")
			} else {
				args = append(args, "-map", "0:a:0")
			}
		}
		args = append(args, "-c", "copy")
	} else {
		path := stored.media.AudioPath
		if path == "" {
			path = stored.media.VideoPath
		} else {
			expectedDuration = stored.audio.duration()
		}
		input(path)
		args = append(args, "-map", "0:a:0", "-vn", "-c:a", "libmp3lame", "-b:a", fmt.Sprintf("%dk", plan.MP3BitrateKbps), "-ar", "44100", "-write_xing", "0")
	}
	args = append(args, "-map_metadata", "-1", "-progress", "pipe:1", "-nostats", output)
	if emit != nil {
		emit(contracts.Progress{Stage: stage})
	}
	err = e.run(ctx, ports.ExecSpec{Executable: ports.FFmpeg, Args: args, Dir: private}, func(line ports.ProcessLine) {
		if emit == nil || line.Stream != ports.Stdout || !strings.HasPrefix(line.Text, "out_time_us=") {
			return
		}
		us, err := strconv.ParseFloat(strings.TrimPrefix(line.Text, "out_time_us="), 64)
		if err != nil || us < 0 || math.IsNaN(us) || math.IsInf(us, 0) {
			return
		}
		seconds := us / 1000000
		p := contracts.Progress{Stage: stage, ProcessedSeconds: &seconds}
		if expectedDuration > 0 {
			duration := expectedDuration
			p.DurationSeconds = &duration
			percent := math.Min(99, seconds/duration*100)
			p.Percent = &percent
		}
		emit(p)
	})
	if err != nil {
		var failure *contracts.ProtocolError
		if errors.As(err, &failure) && failure.Code == contracts.CancelFailed {
			cleanupSafe = false
		}
		return empty, err
	}
	proof, err := e.probeMedia(ctx, output)
	if err != nil {
		var failure *contracts.ProtocolError
		if errors.As(err, &failure) && failure.Code == contracts.CancelFailed {
			cleanupSafe = false
		}
		return empty, err
	}
	if err := validateMediaOutput(proof, choice, plan.MP3BitrateKbps, expectedDuration); err != nil {
		return empty, err
	}
	if emit != nil {
		emit(contracts.Progress{Stage: contracts.StateSaving})
	}
	result, err := finalize(ctx, output, plan.DestinationPath, stored.analysis.Items[0].Title, format, plan.JobID)
	if err != nil {
		return empty, err
	}
	if v := proof.stream("video"); v != nil {
		result.VideoCodec = v.CodecName
	}
	if a := proof.stream("audio"); a != nil {
		result.AudioCodec = a.CodecName
	}
	if choice == nil {
		result.MP3BitrateKbps = plan.MP3BitrateKbps
	}
	return result, nil
}
func validateMediaOutput(p mediaProbe, choice *contracts.VideoChoice, bitrate int, expectedDuration float64) error {
	invalid := func() error {
		return protocolError(contracts.InvalidOutput, "The processed media failed stream or duration validation.")
	}
	duration := p.duration()
	if duration == 0 || (expectedDuration > 0 && math.Abs(duration-expectedDuration) > math.Max(.5, expectedDuration*.01)) {
		return invalid()
	}
	v, a := p.stream("video"), p.stream("audio")
	format := "mp3"
	expectedStreams := 1
	if choice != nil {
		format = choice.Container
		if choice.HasAudio {
			expectedStreams = 2
		}
	}
	allowedContainer := false
	for _, name := range strings.Split(p.Format.FormatName, ",") {
		if format == "mp3" && name == "mp3" || format == "mp4" && name == "mp4" || (format == "webm" || format == "mkv") && (name == "matroska" || name == "webm") {
			allowedContainer = true
		}
	}
	if !allowedContainer || len(p.Streams) != expectedStreams {
		return invalid()
	}
	if choice == nil {
		if v != nil || a == nil || a.CodecName != "mp3" {
			return invalid()
		}
		bps, err := strconv.Atoi(a.Bitrate)
		if err != nil || bps != bitrate*1000 {
			return invalid()
		}
		return nil
	}
	if v == nil || v.CodecName != choice.VideoCodec || choice.HasAudio != (a != nil) {
		return invalid()
	}
	if choice.Width != nil && v.Width != *choice.Width || choice.Height != nil && v.Height != *choice.Height {
		return invalid()
	}
	if a != nil && a.CodecName != choice.AudioCodec {
		return invalid()
	}
	if a != nil {
		vs, ve := strconv.ParseFloat(v.StartTime, 64)
		as, ae := strconv.ParseFloat(a.StartTime, 64)
		if ve == nil && ae == nil && math.Abs(vs-as) > .1 {
			return invalid()
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func fileError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if diskFull(err) {
		return protocolError(contracts.DiskFull, "The destination disk has no space left.")
	}
	return protocolError(contracts.DestinationError, "The media file could not be saved in the destination folder.")
}
func sanitizeTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := strings.Trim(b.String(), " .")
	runes := []rune(s)
	if len(runes) > 120 {
		s = string(runes[:120])
	}
	if s == "" {
		s = "Media"
	}
	base := strings.TrimRight(strings.ToUpper(strings.SplitN(s, ".", 2)[0]), " .")
	device := []rune(base)
	reserved := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(device) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && (device[3] >= '1' && device[3] <= '9' || strings.ContainsRune("¹²³", device[3])))
	if reserved {
		s = "Media " + s
	}
	return s
}

// Always copy into an exclusive destination intermediate before publication.
// This also handles another volume without ever exposing an incomplete final.
func finalize(ctx context.Context, source, destination, title, format, jobID string) (contracts.CompletedFile, error) {
	var empty contracts.CompletedFile
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if format != "mp4" && format != "webm" && format != "mkv" && format != "mp3" {
		return empty, protocolError(contracts.InvalidOutput, "Unsupported output container.")
	}
	if err := validateLocalPath(source, false); err != nil {
		return empty, protocolError(contracts.InvalidOutput, "The processed media file is unavailable.")
	}
	if err := validateLocalPath(destination, true); err != nil {
		return empty, fileError(err)
	}
	in, err := os.Open(source)
	if err != nil {
		return empty, fileError(err)
	}
	defer in.Close()
	sourceInfo, err := in.Stat()
	if err != nil {
		return empty, fileError(err)
	}
	part, owned, err := storage.CreateIntermediate(destination, jobID)
	if err != nil {
		return empty, fileError(err)
	}
	partPath := owned.Path
	published := false
	defer func() {
		_ = part.Close()
		if !published {
			_ = owned.Cleanup()
		}
	}()
	partInfo, err := part.Stat()
	if err != nil {
		return empty, fileError(err)
	}
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(part, digest), contextReader{ctx: ctx, r: in})
	if err != nil {
		return empty, fileError(err)
	}
	if size <= 0 || size != sourceInfo.Size() || uint64(size) > contracts.MaxSafeInteger {
		return empty, protocolError(contracts.InvalidOutput, "The saved media file size is invalid.")
	}
	after, err := in.Stat()
	if err != nil || !os.SameFile(sourceInfo, after) || sourceInfo.ModTime() != after.ModTime() || after.Size() != size {
		return empty, protocolError(contracts.InvalidOutput, "The processed media changed during saving.")
	}
	if err := part.Sync(); err != nil {
		return empty, fileError(err)
	}
	if err := part.Close(); err != nil {
		return empty, fileError(err)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := validateLocalPath(destination, true); err != nil {
		return empty, fileError(err)
	}
	title = sanitizeTitle(title)
	for suffix := 0; suffix < 10000; suffix++ {
		filename := title + "." + format
		if suffix > 0 {
			filename = fmt.Sprintf("%s (%d).%s", title, suffix, format)
		}
		finalPath := filepath.Join(destination, filename)
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if err := publishNoReplace(partPath, finalPath); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return empty, fileError(err)
		}
		published = true
		info, err := os.Lstat(finalPath)
		if err != nil || !info.Mode().IsRegular() || hasReparsePoint(finalPath) || !os.SameFile(info, partInfo) || info.Size() != size {
			return empty, protocolError(contracts.InvalidOutput, "The final media file could not be physically verified.")
		}
		// Publication won the cancellation race. Verify the actual final bytes
		// without turning a physically committed file into a canceled outcome.
		physical, err := proveSource(context.WithoutCancel(ctx), finalPath)
		if err != nil || physical.hash != hex.EncodeToString(digest.Sum(nil)) || !os.SameFile(physical.info, partInfo) {
			// The same intermediate identity remains valid after publication.
			finalOwned := owned
			finalOwned.Path = finalPath
			_ = finalOwned.Cleanup()
			return empty, protocolError(contracts.InvalidOutput, "The final media hash could not be verified.")
		}
		return contracts.CompletedFile{Path: finalPath, Format: format, SizeBytes: uint64(size), SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
	}
	return empty, protocolError(contracts.DestinationError, "The destination contains too many files with this name.")
}
