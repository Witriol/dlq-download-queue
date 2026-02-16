package queue

import (
	"net/url"
	"strings"
)

// IsWebshareJob detects webshare either from explicit site or URL host/path fallback.
func IsWebshareJob(site, rawURL string) bool {
	if strings.EqualFold(strings.TrimSpace(site), "webshare") {
		return true
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	if u, err := url.Parse(rawURL); err == nil {
		host := strings.ToLower(strings.TrimSpace(u.Hostname()))
		if strings.Contains(host, "webshare.cz") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(rawURL), "webshare.cz")
}

// DisplayStatus maps internal paused state to user-facing stopped label for webshare jobs.
func DisplayStatus(status, site, rawURL string) string {
	if status == StatusPaused && IsWebshareJob(site, rawURL) {
		return "stopped"
	}
	return status
}
