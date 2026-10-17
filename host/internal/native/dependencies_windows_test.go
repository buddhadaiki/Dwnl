package native

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	"dwnl/host/internal/testutil"
	"dwnl/host/internal/windows"
)

// This regression catches startup rejecting a missing private executable before
// the real runner/engine can report that dependency through the handshake.
func TestHandshakeDiagnosesMissingPrivateDependencies(t *testing.T) {
	source := os.Getenv("DWNL_MEDIA_BIN")
	if source == "" {
		source = filepath.Join("..", "..", "..", ".tools", "media", "bin")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"yt-dlp", "deno", "ffmpeg", "ffprobe"}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(source, name+".exe")); err != nil {
			t.Skip("private media tools unavailable: " + err.Error())
		}
	}
	fixture := testutil.TempDir(t)
	bin := filepath.Join(fixture, "private bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		input, err := os.Open(filepath.Join(source, name+".exe"))
		if err != nil {
			t.Fatal(err)
		}
		output, err := os.OpenFile(filepath.Join(bin, name+".exe"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			t.Fatal(err)
		}
		_, copyErr := io.Copy(output, input)
		input.Close()
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil {
			t.Fatalf("copy private dependency: copy=%v close=%v", copyErr, closeErr)
		}
	}
	for _, tc := range []struct {
		missing string
		want    contracts.Capabilities
	}{
		{"yt-dlp", contracts.Capabilities{Video: true, MP3: true}},
		{"deno", contracts.Capabilities{Video: true, MP3: true}},
		{"ffmpeg", contracts.Capabilities{NetworkAccess: true}},
		{"ffprobe", contracts.Capabilities{NetworkAccess: true}},
		{"all", contracts.Capabilities{}},
	} {
		t.Run(tc.missing, func(t *testing.T) {
			root := testutil.TempDir(t)
			for _, name := range names {
				if tc.missing == "all" || name == tc.missing {
					present := filepath.Join(bin, name+".exe")
					missing := filepath.Join(fixture, "unavailable-"+name+".exe")
					if err := os.Rename(present, missing); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := os.Rename(missing, present); err != nil {
							t.Error(err)
						}
					})
				}
			}
			runner, err := windows.NewRunner(bin)
			if err != nil {
				t.Fatalf("missing dependency prevented diagnostic handshake: %v", err)
			}
			media, err := engine.New(engine.Config{HostSessionID: "diagnostic-session", Runner: runner, PrivateBinDir: bin, AnalysisRoot: filepath.Join(root, "analyses")})
			if err != nil {
				t.Fatal(err)
			}
			store, err := storage.Open(filepath.Join(root, "state"))
			if err != nil {
				t.Fatal(err)
			}
			dest, err := storage.NewDestinations(filepath.Join(root, "downloads"), nil)
			if err != nil {
				t.Fatal(err)
			}
			session, err := hostruntime.NewSession(hostruntime.Options{
				SessionID: "diagnostic-session", Engine: media, Store: store, Destinations: dest,
				Origin: "chrome-extension://" + extensionIDForDependencyTest + "/", ExtensionID: extensionIDForDependencyTest,
				LoadHandshake: func(ctx context.Context) contracts.HandshakeResult {
					_ = media.Probe(ctx)
					return contracts.HandshakeResult{HostVersion: "0.1.0", EngineVersion: "0.1.0", Dependencies: media.Dependencies(), Capabilities: media.Capabilities()}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			response := session.Handle(ctx, contracts.Request{ProtocolVersion: 1, RequestID: "diagnostic-handshake", Operation: contracts.OperationHandshake, Payload: []byte(`{"extensionVersion":"0.1.0"}`)})
			if response.Error != nil {
				t.Fatalf("diagnostic handshake failed: %v", response.Error)
			}
			result, err := contracts.ResultAs[contracts.HandshakeResult](response)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Dependencies) != 4 || result.Capabilities != tc.want {
				t.Fatalf("diagnostics = %+v; want four dependencies and capabilities %+v", result, tc.want)
			}
			for i, name := range names {
				dependency := result.Dependencies[i]
				wantAvailable := tc.missing != "all" && name != tc.missing
				if dependency.Name != name || dependency.Available != wantAvailable || !wantAvailable && dependency.Version != "unavailable" {
					t.Errorf("dependency %q = %+v; want available=%t", name, dependency, wantAvailable)
				}
			}
		})
	}
}

const extensionIDForDependencyTest = "lphdonkmiecidfgjfplhdfnpphmdioeh"
