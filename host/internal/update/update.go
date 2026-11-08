// Package update finds the latest published Dwnl release and downloads its
// Windows installer, checked against the release's SHA256SUMS.txt and the
// digest GitHub reports for the file. It only runs when the user asks.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxReleaseBytes = 1 << 20
	maxSumsBytes    = 64 << 10
	maxSetupBytes   = 256 << 20
	sumsName        = "SHA256SUMS.txt"
)

// Source is where releases are read from. GitHub returns the real one; tests
// point it at a local server.
type Source struct {
	API       string              // latest release endpoint
	Pages     string              // release pages: <Pages>/<tag>
	Downloads string              // asset files: <Downloads>/<tag>/<name>
	Allowed   func(*url.URL) bool // hosts a request or redirect may reach
	Transport http.RoundTripper
}

func GitHub() Source {
	return Source{
		API:       "https://api.github.com/repos/buddhadaiki/Dwnl/releases/latest",
		Pages:     "https://github.com/buddhadaiki/Dwnl/releases/tag",
		Downloads: "https://github.com/buddhadaiki/Dwnl/releases/download",
		Allowed:   githubHost,
	}
}

// githubHost admits GitHub and the hosts it redirects release files to.
func githubHost(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// Release is a published version and the two files an update needs.
type Release struct {
	Version string // "0.3.1"
	Page    string // its release notes on GitHub
	setup   asset
	sums    asset
}

type asset struct {
	name   string
	url    string
	size   int64
	digest string // "sha256:<hex>" when GitHub reports one
}

// Error carries a message meant for the person using the app.
type Error struct {
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

func fail(message string, cause error) error { return &Error{Message: message, Err: cause} }

const (
	networkMessage  = "Não foi possível falar com o GitHub. Verifique a internet e tente de novo."
	invalidMessage  = "A versão publicada no GitHub não pôde ser verificada. Nada foi instalado."
	mismatchMessage = "O instalador baixado não confere com os hashes publicados. Nada foi instalado."
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,4})\.(0|[1-9][0-9]{0,4})\.(0|[1-9][0-9]{0,4})$`)

// Newer reports whether latest is a later version than current. Both are
// MAJOR.MINOR.PATCH.
func Newer(latest, current string) (bool, error) {
	a, err := parts(latest)
	if err != nil {
		return false, err
	}
	b, err := parts(current)
	if err != nil {
		return false, err
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}

func parts(version string) ([3]int, error) {
	var out [3]int
	match := versionPattern.FindStringSubmatch(version)
	if match == nil {
		return out, fmt.Errorf("invalid version %q", version)
	}
	for i := range out {
		out[i], _ = strconv.Atoi(match[i+1])
	}
	return out, nil
}

func (s Source) client() *http.Client {
	transport := s.Transport
	if transport == nil {
		base := http.DefaultTransport.(*http.Transport).Clone()
		base.ResponseHeaderTimeout = 30 * time.Second
		transport = base
	}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if !s.Allowed(req.URL) {
			return fmt.Errorf("redirect to %s is not allowed", req.URL.Host)
		}
		return nil
	}}
}

func (s Source) get(ctx context.Context, target, accept, userAgent string) (*http.Response, error) {
	u, err := url.Parse(target)
	if err != nil || !s.Allowed(u) {
		return nil, fmt.Errorf("address %q is not allowed", target)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", userAgent)
	return s.client().Do(req)
}

// Latest reads the newest published release.
func (s Source) Latest(ctx context.Context, userAgent string) (Release, error) {
	response, err := s.get(ctx, s.API, "application/vnd.github+json", userAgent)
	if err != nil {
		return Release{}, fail(networkMessage, err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return Release{}, fail("Ainda não há versão publicada no GitHub.", nil)
	case response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests:
		return Release{}, fail("O GitHub limitou as consultas por agora. Tente de novo em alguns minutos.", nil)
	case response.StatusCode != http.StatusOK:
		return Release{}, fail(networkMessage, fmt.Errorf("status %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBytes+1))
	if err != nil {
		return Release{}, fail(networkMessage, err)
	}
	if len(body) > maxReleaseBytes {
		return Release{}, fail(invalidMessage, errors.New("release metadata too large"))
	}
	var published struct {
		Tag        string `json:"tag_name"`
		Page       string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &published); err != nil {
		return Release{}, fail(invalidMessage, err)
	}
	version := strings.TrimPrefix(published.Tag, "v")
	if published.Draft || published.Prerelease || published.Tag != "v"+version || !versionPattern.MatchString(version) || published.Page != s.Pages+"/"+published.Tag {
		return Release{}, fail(invalidMessage, fmt.Errorf("unexpected release %q", published.Tag))
	}
	release := Release{Version: version, Page: published.Page}
	setupName := "Dwnl-Setup-" + version + "-windows-x64.exe"
	for _, a := range published.Assets {
		if a.Name != setupName && a.Name != sumsName {
			continue
		}
		limit := int64(maxSetupBytes)
		if a.Name == sumsName {
			limit = maxSumsBytes
		}
		if a.URL != s.Downloads+"/"+published.Tag+"/"+a.Name || a.Size <= 0 || a.Size > limit {
			return Release{}, fail(invalidMessage, fmt.Errorf("unexpected asset %q", a.Name))
		}
		found := asset{name: a.Name, url: a.URL, size: a.Size, digest: a.Digest}
		if a.Name == setupName {
			release.setup = found
		} else {
			release.sums = found
		}
	}
	if release.setup.name == "" || release.sums.name == "" {
		return Release{}, fail("A versão "+version+" ainda não tem o instalador para Windows publicado.", nil)
	}
	return release, nil
}

// Fetch downloads the release's installer into dir and returns its path once
// its size and SHA-256 match SHA256SUMS.txt and GitHub's digest.
func (s Source) Fetch(ctx context.Context, release Release, dir, userAgent string) (string, error) {
	if release.setup.name == "" {
		return "", fail(invalidMessage, errors.New("release was not read with Latest"))
	}
	sums, err := s.read(ctx, release.sums, userAgent)
	if err != nil {
		return "", err
	}
	expected, ok := sumFor(sums, release.setup.name)
	if !ok {
		return "", fail(invalidMessage, errors.New("installer is missing from SHA256SUMS.txt"))
	}
	if release.setup.digest != "" && !strings.EqualFold(release.setup.digest, "sha256:"+expected) {
		return "", fail(mismatchMessage, errors.New("GitHub digest differs from SHA256SUMS.txt"))
	}
	response, err := s.get(ctx, release.setup.url, "application/octet-stream", userAgent)
	if err != nil {
		return "", fail(networkMessage, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fail(networkMessage, fmt.Errorf("status %d", response.StatusCode))
	}
	part, err := os.CreateTemp(dir, "Dwnl-Setup-*.part")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			part.Close()
			os.Remove(part.Name())
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(part, hash), io.LimitReader(response.Body, release.setup.size+1))
	if err != nil {
		return "", fail(networkMessage, err)
	}
	if written != release.setup.size || hex.EncodeToString(hash.Sum(nil)) != expected {
		return "", fail(mismatchMessage, fmt.Errorf("downloaded %d bytes", written))
	}
	if err := part.Sync(); err != nil {
		return "", err
	}
	if err := part.Close(); err != nil {
		return "", err
	}
	final := filepath.Join(dir, release.setup.name)
	if err := os.Remove(final); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(part.Name(), final); err != nil {
		return "", err
	}
	keep = true
	return final, nil
}

func (s Source) read(ctx context.Context, a asset, userAgent string) ([]byte, error) {
	response, err := s.get(ctx, a.url, "application/octet-stream", userAgent)
	if err != nil {
		return nil, fail(networkMessage, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fail(networkMessage, fmt.Errorf("status %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSumsBytes+1))
	if err != nil {
		return nil, fail(networkMessage, err)
	}
	if len(body) > maxSumsBytes {
		return nil, fail(invalidMessage, errors.New("SHA256SUMS.txt too large"))
	}
	return body, nil
}

var sumLine = regexp.MustCompile(`^([0-9a-f]{64}) [ *](.+)$`)

// sumFor reads "<sha256>  <name>" lines, as sha256sum writes them.
func sumFor(sums []byte, name string) (string, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimPrefix(sums, []byte("\xef\xbb\xbf"))))
	for scanner.Scan() {
		match := sumLine.FindStringSubmatch(strings.TrimRight(scanner.Text(), "\r"))
		if match != nil && match[2] == name {
			return match[1], true
		}
	}
	return "", false
}
