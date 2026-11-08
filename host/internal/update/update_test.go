package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testSetup = "Dwnl-Setup-0.4.0-windows-x64.exe"

// fixture is a local GitHub: a latest-release endpoint, the two assets, and a
// redirect from the asset address to where its bytes live.
type fixture struct {
	server     *httptest.Server
	outside    *httptest.Server // a host the policy must refuse, serving the right bytes
	setup      []byte
	served     []byte // bytes the file host sends, setup unless changed
	sums       string
	release    map[string]any
	status     int
	redirectTo string
	setupHits  atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{setup: []byte("MZ installer bytes for 0.4.0"), status: http.StatusOK}
	f.served = f.setup
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	f.outside = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(f.setup) }))
	t.Cleanup(f.outside.Close)
	sum := sha256.Sum256(f.setup)
	f.sums = "\xef\xbb\xbf" + strings.Repeat("0", 64) + "  Dwnl-0.4.0-windows-x64.zip\r\n" + hex.EncodeToString(sum[:]) + " *" + testSetup + "\r\n"
	base := f.server.URL
	f.release = map[string]any{
		"tag_name": "v0.4.0", "html_url": base + "/pages/v0.4.0", "draft": false, "prerelease": false,
		"assets": []map[string]any{
			{"name": "Dwnl-0.4.0-windows-x64.zip", "browser_download_url": base + "/download/v0.4.0/Dwnl-0.4.0-windows-x64.zip", "size": 10},
			{"name": testSetup, "browser_download_url": base + "/download/v0.4.0/" + testSetup, "size": len(f.setup), "digest": "sha256:" + hex.EncodeToString(sum[:])},
			{"name": sumsName, "browser_download_url": base + "/download/v0.4.0/" + sumsName, "size": len(f.sums)},
		},
	}
	f.redirectTo = base + "/objects/setup"
	return f
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/latest":
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			return
		}
		_ = json.NewEncoder(w).Encode(f.release)
	case "/download/v0.4.0/" + sumsName:
		_, _ = w.Write([]byte(f.sums))
	case "/download/v0.4.0/" + testSetup:
		f.setupHits.Add(1)
		http.Redirect(w, r, f.redirectTo, http.StatusFound)
	case "/objects/setup":
		_, _ = w.Write(f.served)
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) source() Source {
	host := f.server.Listener.Addr().String()
	return Source{
		API: f.server.URL + "/api/latest", Pages: f.server.URL + "/pages", Downloads: f.server.URL + "/download",
		Allowed:   func(u *url.URL) bool { return u.Scheme == "https" && u.Host == host },
		Transport: f.server.Client().Transport,
	}
}

func (f *fixture) setAsset(name, key string, value any) {
	for _, a := range f.release["assets"].([]map[string]any) {
		if a["name"] == name {
			a[key] = value
		}
	}
}

func userMessage(t *testing.T, err error) string {
	t.Helper()
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("error without a user message: %v", err)
	}
	return typed.Message
}

func emptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("left %d files behind, first %q", len(entries), entries[0].Name())
	}
}

func TestUpdateDownloadsTheVerifiedInstallerOfTheLatestRelease(t *testing.T) {
	f := newFixture(t)
	release, err := f.source().Latest(context.Background(), "Dwnl/0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "0.4.0" || release.Page != f.server.URL+"/pages/v0.4.0" {
		t.Fatalf("release = %+v", release)
	}
	dir := t.TempDir()
	path, err := f.source().Fetch(context.Background(), release, dir, "Dwnl/0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(f.setup) || filepath.Base(path) != testSetup {
		t.Fatalf("installer %q = %q, %v", path, got, err)
	}
	// A second download replaces the first and leaves no partial file.
	if _, err := f.source().Fetch(context.Background(), release, dir, "Dwnl/0.3.0"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("files after two downloads: %v", entries)
	}
}

func TestUpdateRefusesAnInstallerThatDoesNotMatchItsHashes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(*fixture)
		hits   int32
	}{
		{"other bytes", func(f *fixture) { f.served = []byte("MZ installer bytes for 0.4.1") }, 1},
		{"extra bytes", func(f *fixture) { f.served = append(append([]byte{}, f.setup...), 'x') }, 1},
		{"short file", func(f *fixture) { f.served = f.setup[:len(f.setup)-1] }, 1},
		{"SHA256SUMS disagrees", func(f *fixture) {
			f.sums = strings.Repeat("a", 64) + "  " + testSetup + "\n"
			f.setAsset(sumsName, "size", len(f.sums))
			f.setAsset(testSetup, "digest", "")
		}, 1},
		{"GitHub digest disagrees", func(f *fixture) { f.setAsset(testSetup, "digest", "sha256:"+strings.Repeat("b", 64)) }, 0},
		{"installer missing from SHA256SUMS", func(f *fixture) {
			f.sums = strings.Repeat("0", 64) + "  other.exe\n"
			f.setAsset(sumsName, "size", len(f.sums))
		}, 0},
		{"redirect leaves GitHub", func(f *fixture) { f.redirectTo = f.outside.URL + "/setup" }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.damage(f)
			release, err := f.source().Latest(context.Background(), "Dwnl/0.3.0")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			_, err = f.source().Fetch(context.Background(), release, dir, "Dwnl/0.3.0")
			if err == nil {
				t.Fatal("the installer was accepted")
			}
			if message := userMessage(t, err); message != mismatchMessage && message != invalidMessage && message != networkMessage {
				t.Fatalf("message %q", message)
			}
			if got := f.setupHits.Load(); got != tc.hits {
				t.Fatalf("installer requested %d times, want %d", got, tc.hits)
			}
			emptyDir(t, dir)
		})
	}
}

func TestUpdateRefusesReleaseMetadataItCannotVouchFor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(*fixture)
		want   string
	}{
		{"prerelease", func(f *fixture) { f.release["prerelease"] = true }, invalidMessage},
		{"tag that is not a version", func(f *fixture) { f.release["tag_name"] = "v0.4" }, invalidMessage},
		{"page of another repository", func(f *fixture) { f.release["html_url"] = "https://github.com/someone/else/releases/tag/v0.4.0" }, invalidMessage},
		{"installer from another address", func(f *fixture) {
			f.setAsset(testSetup, "browser_download_url", "https://github.com/someone/else/releases/download/v0.4.0/"+testSetup)
		}, invalidMessage},
		{"empty installer", func(f *fixture) { f.setAsset(testSetup, "size", 0) }, invalidMessage},
		{"no installer", func(f *fixture) { f.setAsset(testSetup, "name", "Dwnl-Setup-0.4.0-linux.tar") }, "A versão 0.4.0 ainda não tem o instalador para Windows publicado."},
		{"rate limited", func(f *fixture) { f.status = http.StatusForbidden }, "O GitHub limitou as consultas por agora. Tente de novo em alguns minutos."},
		{"nothing published", func(f *fixture) { f.status = http.StatusNotFound }, "Ainda não há versão publicada no GitHub."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.damage(f)
			_, err := f.source().Latest(context.Background(), "Dwnl/0.3.0")
			if err == nil {
				t.Fatal("the release was accepted")
			}
			if got := userMessage(t, err); got != tc.want {
				t.Fatalf("message %q, want %q", got, tc.want)
			}
		})
	}
	f := newFixture(t)
	source := f.source()
	source.Allowed = githubHost
	if _, err := source.Latest(context.Background(), "Dwnl/0.3.0"); err == nil || userMessage(t, err) != networkMessage {
		t.Fatalf("an address outside GitHub was read: %v", err)
	}
}

func TestUpdateComparesVersionsNumerically(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.3.1", "0.3.0", true},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.3.0", "0.3.0", false},
		{"0.2.9", "0.3.0", false},
	} {
		got, err := Newer(tc.latest, tc.current)
		if err != nil || got != tc.want {
			t.Fatalf("Newer(%s, %s) = %v, %v", tc.latest, tc.current, got, err)
		}
	}
	for _, bad := range []string{"", "v0.3.0", "0.3", "0.3.0-beta", "01.2.3", "0.3.0 "} {
		if _, err := Newer(bad, "0.3.0"); err == nil {
			t.Fatalf("version %q was accepted", bad)
		}
	}
}

func TestUpdateAllowsOnlyGitHubOverHTTPS(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://api.github.com/repos/buddhadaiki/Dwnl/releases/latest":  true,
		"https://github.com/buddhadaiki/Dwnl/releases/download/v0.4.0/x": true,
		"https://release-assets.githubusercontent.com/x":                 true,
		"https://objects.githubusercontent.com/x":                        true,
		"http://github.com/x":                         false,
		"https://github.com:8443/x":                   false,
		"https://user@github.com/x":                   false,
		"https://github.com.example.com/x":            false,
		"https://githubusercontent.com.example.com/x": false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if githubHost(u) != want {
			t.Fatalf("githubHost(%s) = %v", raw, !want)
		}
	}
}
