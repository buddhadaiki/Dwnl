// dwnl-online-probe is a development-only client of the real engine/Job runner.
// It writes only below the workspace .tools/online-real directory, never the
// installed host's AppData paths or a Chrome profile. Not packaged in setup.
package main

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	"dwnl/host/internal/windows"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	download := len(os.Args) == 3 && os.Args[2] == "--download-authorized-video"
	if len(os.Args) != 2 && !download {
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
	bin := filepath.Join(root, ".tools", "media", "bin")
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
		if len(trace) < 128 {
			trace = append(trace, line)
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
	if download && err == nil {
		if len(analysis.Items) != 1 || len(analysis.Items[0].VideoChoices) == 0 {
			panic("a single playable item is required")
		}
		item := analysis.Items[0]
		choice := item.VideoChoices[0]
		destination := filepath.Join(work, "authorized-downloads")
		jobWork := filepath.Join(work, "job-"+analysis.AnalysisID)
		if err = os.MkdirAll(destination, 0700); err == nil {
			err = os.Mkdir(jobWork, 0700)
		}
		if err == nil {
			completed, err = media.Execute(ctx, contracts.DownloadPlan{HostSessionID: analysis.HostSessionID, Analysis: analysis, Item: item, Mode: contracts.ModeVideo, VideoChoice: &choice, DestinationID: "development-approved", DestinationPath: destination, WorkDir: jobWork, JobID: analysis.AnalysisID}, nil)
		}
		if err == nil {
			_ = os.Remove(jobWork)
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
	report := map[string]any{"source": source, "elapsedSeconds": time.Since(start).Seconds(), "analysis": analysis, "errorCode": code, "error": message, "transportTrace": trace, "downloadAttempted": download, "completedFile": completed, "capabilities": media.Capabilities(), "probeError": fmt.Sprint(probeErr), "installedOrRegistered": false}
	body, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(body))
}
