package engine

import (
	"dwnl/host/internal/contracts"
	"net/url"
	"strings"
)

// CanonicalSourceURL accepts exact official input hosts and strips all query
// fields except the media identifier. This check grants no network authority.
func CanonicalSourceURL(raw string) (string, error) {
	invalid := func() (string, error) {
		return "", protocolError(contracts.InvalidMessage, "Use a supported HTTPS media URL without credentials.")
	}
	if len(raw) > 4096 || raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\ \t\r\n") || !strings.HasPrefix(raw, "https://") {
		return invalid()
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return invalid()
	}
	host := strings.ToLower(u.Hostname())
	platform := map[string]string{"youtube.com": "youtube", "www.youtube.com": "youtube", "m.youtube.com": "youtube", "music.youtube.com": "youtube", "youtu.be": "youtube", "instagram.com": "instagram", "www.instagram.com": "instagram", "tiktok.com": "tiktok", "www.tiktok.com": "tiktok", "vm.tiktok.com": "tiktok", "vt.tiktok.com": "tiktok", "x.com": "x", "www.x.com": "x", "twitter.com": "x", "www.twitter.com": "x"}[host]
	if platform == "" {
		return invalid()
	}
	// Ambiguous escaped separators, controls, or path traversal are not media IDs.
	if strings.ContainsAny(u.Path, "\\\x00\r\n") || strings.Contains(u.Path, "/../") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(u.EscapedPath()), "%5c") {
		return invalid()
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return invalid()
	}
	clean := url.Values{}
	if platform == "youtube" && u.Path == "/watch" {
		if len(q["v"]) != 1 || q.Get("v") == "" || len(q.Get("v")) > 128 {
			return invalid()
		}
		clean.Set("v", q.Get("v"))
	}
	u.Host = host
	u.RawQuery = clean.Encode()
	u.Fragment = ""
	u.ForceQuery = false
	return u.String(), nil
}
