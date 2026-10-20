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
		fmt.Fprintln(os.Stderr, failureDiagnostic(err))
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 || os.Args[1] != "chrome-extension://"+extensionID+"/" {
		return failAt("origin", fmt.Errorf("unauthorized native origin"))
	}
	// Chrome may append its parent-window argument. It grants no path authority.
	executable, err := os.Executable()
	if err != nil {
		return failAt("paths", err)
	}
	releaseUse, err := acquireRuntimeUse(executable)
	if err != nil {
		return failAt("installation", err)
	}
	defer releaseUse()
	runner, err := windows.NewRunner(filepath.Join(filepath.Dir(executable), "bin"))
	if err != nil {
		return failAt("runner", err)
	}
	sessionID := runtime.NewID()
	stateRoot, defaultPath, err := windows.DefaultPaths()
	if err != nil {
		return failAt("paths", err)
	}
	media, err := engine.New(engine.Config{HostSessionID: sessionID, Runner: runner, PrivateBinDir: filepath.Join(filepath.Dir(executable), "bin"), AnalysisRoot: filepath.Join(stateRoot, "analyses")})
	if err != nil {
		return failAt("engine", err)
	}
	store, err := openHostStore(stateRoot)
	if err != nil {
		return failAt("store", err)
	}
	preferredPath, preferenceErr := store.PreferredDestination(defaultPath)
	if preferenceErr != nil {
		// Keep the fallback visible in the handshake; private corruption is
		// reported without leaking saved paths or resuming previous work.
		fmt.Fprintln(os.Stderr, "Dwnl is using the default destination because the saved preference could not be validated")
		if preferredPath == "" {
			return failAt("destination", preferenceErr)
		}
	}
	defaultPath = preferredPath
	destinations, err := storage.NewDestinations(defaultPath, nil)
	if err != nil {
		return failAt("destination", err)
	}
	session, err := runtime.NewSession(runtime.Options{SessionID: sessionID, Engine: media, Store: store, Destinations: destinations, Origin: os.Args[1], ExtensionID: extensionID, LoadHandshake: func(ctx context.Context) contracts.HandshakeResult {
		// Probe only after Serve observes the connection, so EOF also cancels
		// dependency probes. Missing tools leave truthful capabilities false.
		_ = media.Probe(ctx)
		return contracts.HandshakeResult{HostVersion: version, EngineVersion: version, Dependencies: media.Dependencies(), Capabilities: media.Capabilities()}
	}})
	if err != nil {
		return failAt("session", err)
	}
	defer session.Close()
	return failAt("transport", native.Serve(context.Background(), os.Stdin, os.Stdout, session))
}

func openHostStore(root string) (*storage.Store, error) {
	store, err := storage.Open(root)
	if err != nil {
		return nil, err
	}
	// The first dependency probe creates analyses beneath state even when no
	// job or preference is saved. Claim ownership before that first write.
	if err = store.InitializeOwnership(); err != nil {
		return nil, err
	}
	return store, nil
}
