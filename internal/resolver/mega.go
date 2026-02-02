package resolver

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type megaResolver struct{}

func NewMegaResolver() Resolver {
	return &megaResolver{}
}

func (r *megaResolver) CanHandle(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return strings.Contains(host, "mega.nz") || strings.Contains(host, "mega.co.nz")
}

func (r *megaResolver) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	// Placeholder: MEGA resolution is handled by a dedicated downloader (MEGAcmd or SDK).
	// Return a typed error so the daemon can report it cleanly.
	return nil, errors.New("mega_resolver_not_configured")
}
