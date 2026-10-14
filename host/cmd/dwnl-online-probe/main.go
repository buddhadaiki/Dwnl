// dwnl-online-probe is a development-only client of the real engine/Job runner.
// It writes only below the workspace .tools/online-real directory, never the
// installed host's AppData paths or a Chrome profile. Not packaged in setup.
package main

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	"dwnl/host/internal/ports"
	"dwnl/host/internal/windows"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	download := len(os.Args) == 3 && os.Args[2] == "--download-authorized-video"
	publicSample := len(os.Args) == 3 && os.Args[2] == "--download-public-sample"
	mp3Only := len(os.Args) == 3 && os.Args[2] == "--download-public-mp3"
	if len(os.Args) != 2 && !download && !publicSample && !mp3Only {
		panic("one public HTTPS source URL is required")
	}
	source, err := engine.CanonicalSourceURL(os.Args[1])
	if err != nil {
		panic(err)
	}
	root, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	bin := filepath.Join(root, ".tools", "bootstrap-runtime-20261014", "bin")
	work := filepath.Join(root, ".tools", "online-real")
	if err = os.MkdirAll(work, 0700); err != nil {
		panic(err)
	}
	runner, err := windows.NewRunner(bin)
	if err != nil {
		panic(err)
	}
	trace := []string{}
	media, err := engine.New(engine.Config{HostSessionID: "development-probe", Runner: runner, PrivateBinDir: bin, AnalysisRoot: work, NetworkTrace: func(line string) {
		if strings.HasPrefix(line, "DWNL_PROGRESS ") {
			return
		}
		if len(trace) < 128 {
			trace = append(trace, line)
		} else {
			copy(trace[64:], trace[65:])
			trace[127] = line
		}
	}})
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	start := time.Now()
	probeErr := media.Probe(ctx)
	analysis, err := media.Analyze(ctx, source)
	var completed contracts.CompletedFile
	outputs := []map[string]any{}
	if (download || publicSample || mp3Only) && err == nil {
		if len(analysis.Items) != 1 || len(analysis.Items[0].VideoChoices) == 0 {
			panic("a single playable item is required")
		}
		item := analysis.Items[0]
		choice := item.VideoChoices[0]
		for _, candidate := range item.VideoChoices {
			if candidate.Height != nil && *candidate.Height <= 1080 && (choice.Height == nil || *choice.Height > 1080 || *candidate.Height > *choice.Height) {
				choice = candidate
			}
		}
		destination := filepath.Join(work, "authorized-downloads")
		modes := []contracts.DownloadMode{contracts.ModeVideo}
		if mp3Only {
			modes = []contracts.DownloadMode{contracts.ModeMP3}
		}
		if publicSample {
			modes = append(modes, contracts.ModeMP3)
		}
		for _, mode := range modes {
			if mode == contracts.ModeMP3 && !item.HasAudio && !item.AudioUnknown {
				outputs = append(outputs, map[string]any{"mode": mode, "attempted": false, "reason": "source metadata declares no audio"})
				continue
			}
			jobID := analysis.AnalysisID + "-" + string(mode)
			jobWork := filepath.Join(work, "job-"+jobID)
			var modeErr error
			if modeErr = os.MkdirAll(destination, 0700); modeErr == nil {
				modeErr = os.Mkdir(jobWork, 0700)
			}
			var selected *contracts.VideoChoice
			if mode == contracts.ModeVideo {
				copy := choice
				selected = &copy
			}
			if modeErr == nil {
				completed, modeErr = media.Execute(ctx, contracts.DownloadPlan{HostSessionID: analysis.HostSessionID, Analysis: analysis, Item: item, Mode: mode, VideoChoice: selected, MP3BitrateKbps: 192, DestinationID: "development-approved", DestinationPath: destination, WorkDir: jobWork, JobID: jobID}, nil)
			}
			output := map[string]any{"mode": mode, "attempted": true, "completedFile": completed, "error": errorText(modeErr), "errorCode": errorCode(modeErr)}
			if modeErr == nil {
				var raw strings.Builder
				result, probeReadErr := runner.Run(ctx, ports.ExecSpec{Executable: ports.FFprobe, Args: []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,wav,mp3", "-show_format", "-show_streams", "-of", "json", completed.Path}}, func(line ports.ProcessLine) {
					if line.Stream == ports.Stdout {
						raw.WriteString(line.Text)
						raw.WriteByte('\n')
					}
				})
				output["ffprobeExitCode"] = result.ExitCode
				output["ffprobeError"] = errorText(probeReadErr)
				if json.Valid([]byte(raw.String())) {
					output["ffprobe"] = json.RawMessage(raw.String())
				}
			}
			outputs = append(outputs, output)
			if modeErr != nil && err == nil {
				err = modeErr
			}
			if errorCode(modeErr) != string(contracts.CancelFailed) {
				_ = os.Remove(jobWork)
			}
			if errorCode(modeErr) == string(contracts.AccessRestricted) || errorCode(modeErr) == string(contracts.CancelFailed) || ctx.Err() != nil {
				break
			}
		}
	}
	code := ""
	var protocol *contracts.ProtocolError
	if errors.As(err, &protocol) {
		code = string(protocol.Code)
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	report := map[string]any{"source": source, "elapsedSeconds": time.Since(start).Seconds(), "analysis": analysis, "errorCode": code, "error": message, "transportTrace": trace, "downloadAttempted": download || publicSample || mp3Only, "outputs": outputs, "capabilities": media.Capabilities(), "probeError": fmt.Sprint(probeErr), "installedOrRegistered": false}
	body, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(body))
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func errorCode(err error) string {
	var p *contracts.ProtocolError
	if errors.As(err, &p) {
		return string(p.Code)
	}
	return ""
}
