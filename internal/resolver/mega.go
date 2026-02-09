package resolver

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// megaResolver is a stub. MEGA downloads require a dedicated tool (MEGAcmd or
// the MEGA SDK) that is not yet integrated. The resolver recognises mega.nz
// URLs so they get a clear error instead of falling through to the HTTP
// resolver.
//
// TODO: integrate MEGAcmd CLI or a Go SDK wrapper to support MEGA downloads.
// TODO: add credential handling via MEGA_EMAIL / MEGA_PASSWORD env vars.
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
	return nil, errors.New("mega_resolver_not_configured")
}
