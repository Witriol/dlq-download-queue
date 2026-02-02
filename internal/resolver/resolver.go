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
)

type ResolvedTarget struct {
	Kind     string
	URL      string
	Headers  map[string]string
	Filename string
	Size     int64
}

type Resolver interface {
	CanHandle(rawURL string) bool
	Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error)
}

type Registry struct {
	resolvers []Resolver
}

func NewRegistry(resolvers ...Resolver) *Registry {
	return &Registry{resolvers: resolvers}
}

func (r *Registry) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	for _, res := range r.resolvers {
		if res.CanHandle(rawURL) {
			return res.Resolve(ctx, rawURL)
		}
	}
	return nil, errors.New("no_resolver")
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
