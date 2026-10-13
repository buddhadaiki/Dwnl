// dwnl-test-harness is an internal validation tool. It is never bundled with the
// Native Messaging host or installer and cannot write outside the project.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	"dwnl/host/internal/native"
	"dwnl/host/internal/ports"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	hostwindows "dwnl/host/internal/windows"
)

const extensionID = "lphdonkmiecidfgjfplhdfnpphmdioeh"
const fixtureURL = "https://youtu.be/dwnl-local-fixture"

type fixtureEngine struct {
	engine *engine.Engine
	root   string
}

func (f fixtureEngine) Analyze(ctx context.Context, url string) (contracts.Analysis, error) {
	if url != fixtureURL {
		return f.engine.Analyze(ctx, url)
	}
	return f.engine.ImportLocal(ctx, url, engine.LocalMedia{VideoPath: filepath.Join(f.root, "source.mp4"), Title: "Dwnl synthetic local fixture"})
}
func (f fixtureEngine) Execute(ctx context.Context, plan contracts.DownloadPlan, progress func(contracts.Progress)) (contracts.CompletedFile, error) {
	return f.engine.Execute(ctx, plan, progress)
}

func run() error {
	if len(os.Args) != 4 {
		return errors.New("test harness expects isolated root, private binary directory and origin")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := filepath.Abs(os.Args[1])
	if err != nil {
		return err
	}
	base := filepath.Join(cwd, ".tools", "tmp")
	if filepath.Dir(root) != base || !strings.HasPrefix(filepath.Base(root), "dwnl-native-integration-") {
		return errors.New("test root must belong to this project's isolated integration directory")
	}
	if err := hostwindows.CheckPath(root, false); err != nil {
		return err
	}
	bins := filepath.Join(cwd, ".tools", "media", "bin")
	if filepath.Clean(os.Args[2]) != bins {
		return errors.New("unexpected test binary directory")
	}
	if os.Args[3] != "chrome-extension://"+extensionID+"/" {
		return errors.New("forbidden origin")
	}
	runner, err := hostwindows.NewRunner(bins)
	if err != nil {
		return err
	}
	ctx := context.Background()
	source := filepath.Join(root, "source.mp4")
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		result, err := runner.Run(ctx, ports.ExecSpec{Executable: ports.FFmpeg, Dir: root, Args: []string{
			"-nostdin", "-hide_banner", "-loglevel", "error", "-n", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25:duration=1.5",
			"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1.5", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", source,
		}}, func(ports.ProcessLine) {})
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return errors.New("synthetic fixture generation failed")
		}
	} else if err != nil {
		return err
	}
	sessionID := hostruntime.NewID()
	media, err := engine.New(engine.Config{HostSessionID: sessionID, Runner: runner})
	if err != nil {
		return err
	}
	if err := media.Probe(ctx); err != nil {
		return fmt.Errorf("dependency probe (%v): %w", media.Dependencies(), err)
	}
	store, err := storage.Open(filepath.Join(root, "ledger"))
	if err != nil {
		return err
	}
	destinations, err := storage.NewDestinations(filepath.Join(root, "downloads"), func(context.Context) (string, bool, error) {
		return filepath.Join(root, "downloads"), true, nil
	})
	if err != nil {
		return err
	}
	session, err := hostruntime.NewSession(hostruntime.Options{
		SessionID: sessionID, Engine: fixtureEngine{media, root}, Store: store, Destinations: destinations,
		Origin: os.Args[3], ExtensionID: extensionID,
		Handshake: contracts.HandshakeResult{HostVersion: "0.1.0-test-harness", EngineVersion: "local-fixture-only", Dependencies: media.Dependencies(), Capabilities: media.Capabilities(), DefaultDestination: destinations.Default()},
	})
	if err != nil {
		return err
	}
	defer session.Close()
	return native.Serve(ctx, os.Stdin, os.Stdout, session)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Dwnl isolated validation failed:", err)
		os.Exit(1)
	}
}
