// dwnl-host is a stdio-only native host. Chrome supplies the extension origin;
// executable paths and runtime flags cannot be supplied by the extension.
package main

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	"dwnl/host/internal/native"
	"dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	"dwnl/host/internal/windows"
	"fmt"
	"os"
	"path/filepath"
)

const extensionID = "lphdonkmiecidfgjfplhdfnpphmdioeh"
const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Dwnl host could not continue")
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 || os.Args[1] != "chrome-extension://"+extensionID+"/" {
		return fmt.Errorf("unauthorized native origin")
	}
	// Chrome may append its parent-window argument. It grants no path authority.
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	runner, err := windows.NewRunner(filepath.Join(filepath.Dir(executable), "bin"))
	if err != nil {
		return err
	}
	sessionID := runtime.NewID()
	media, err := engine.New(engine.Config{HostSessionID: sessionID, Runner: runner})
	if err != nil {
		return err
	}
	stateRoot, defaultPath, err := windows.DefaultPaths()
	if err != nil {
		return err
	}
	store, err := storage.Open(stateRoot)
	if err != nil {
		return err
	}
	preferredPath, preferenceErr := store.PreferredDestination(defaultPath)
	if preferenceErr != nil {
		// Keep the fallback visible in the handshake; private corruption is
		// reported without leaking saved paths or resuming previous work.
		fmt.Fprintln(os.Stderr, "Dwnl is using the default destination because the saved preference could not be validated")
		if preferredPath == "" {
			return preferenceErr
		}
	}
	defaultPath = preferredPath
	destinations, err := storage.NewDestinations(defaultPath, nil)
	if err != nil {
		return err
	}
	session, err := runtime.NewSession(runtime.Options{SessionID: sessionID, Engine: media, Store: store, Destinations: destinations, Origin: os.Args[1], ExtensionID: extensionID, LoadHandshake: func(ctx context.Context) contracts.HandshakeResult {
		// Probe only after Serve observes the connection, so EOF also cancels
		// dependency probes. Missing tools leave truthful capabilities false.
		_ = media.Probe(ctx)
		return contracts.HandshakeResult{HostVersion: version, EngineVersion: version, Dependencies: media.Dependencies(), Capabilities: media.Capabilities()}
	}})
	if err != nil {
		return err
	}
	defer session.Close()
	return native.Serve(context.Background(), os.Stdin, os.Stdout, session)
}
