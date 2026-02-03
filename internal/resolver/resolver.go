package resolver

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrLoginRequired  = errors.New("login_required")
	ErrQuotaExceeded  = errors.New("quota_exceeded")
	ErrCaptchaNeeded  = errors.New("captcha_needed")
	ErrTemporarilyOff = errors.New("temporarily_unavailable")
	ErrUnknownSite    = errors.New("unknown_site")
)

type ResolvedTarget struct {
	Kind     string
	URL      string
	Headers  map[string]string
	Options  map[string]string
	Filename string
	Size     int64
}

type Resolver interface {
	CanHandle(rawURL string) bool
	Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error)
}

type Registry struct {
	resolvers     []Resolver
	siteResolvers map[string]Resolver
}

func NewRegistry(resolvers ...Resolver) *Registry {
	return &Registry{
		resolvers:     resolvers,
		siteResolvers: map[string]Resolver{},
	}
}

func (r *Registry) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	for _, res := range r.resolvers {
		if res.CanHandle(rawURL) {
			return res.Resolve(ctx, rawURL)
		}
	}
	return nil, errors.New("no_resolver")
}

func (r *Registry) RegisterSite(name string, res Resolver) {
	if res == nil {
		return
	}
	if r.siteResolvers == nil {
		r.siteResolvers = map[string]Resolver{}
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return
	}
	r.siteResolvers[key] = res
}

func (r *Registry) ResolveWithSite(ctx context.Context, site, rawURL string) (*ResolvedTarget, error) {
	if site = strings.TrimSpace(site); site != "" {
		key := strings.ToLower(site)
		if res, ok := r.siteResolvers[key]; ok {
			return res.Resolve(ctx, rawURL)
		}
		return nil, ErrUnknownSite
	}
	return r.Resolve(ctx, rawURL)
}

// NewHTTPResolver returns a pass-through resolver for direct HTTP/HTTPS URLs.
func NewHTTPResolver() Resolver {
	return &httpResolver{}
}

type httpResolver struct{}

func (r *httpResolver) CanHandle(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")
}

func (r *httpResolver) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	return &ResolvedTarget{
		Kind: "aria2",
		URL:  rawURL,
	}, nil
}
