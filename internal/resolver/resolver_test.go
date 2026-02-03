package resolver

import (
	"context"
	"testing"
)

type stubResolver struct {
	lastURL string
}

func (s *stubResolver) CanHandle(rawURL string) bool { return true }
func (s *stubResolver) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	s.lastURL = rawURL
	return &ResolvedTarget{Kind: "aria2", URL: rawURL}, nil
}

func TestResolveWithSite(t *testing.T) {
	ctx := context.Background()
	siteRes := &stubResolver{}
	reg := NewRegistry(siteRes)
	reg.RegisterSite("webshare", siteRes)

	if _, err := reg.ResolveWithSite(ctx, "webshare", "https://example.com/a"); err != nil {
		t.Fatalf("resolve with site: %v", err)
	}
	if siteRes.lastURL != "https://example.com/a" {
		t.Fatalf("expected resolver to be used")
	}
	if _, err := reg.ResolveWithSite(ctx, "unknown", "https://example.com/a"); err == nil {
		t.Fatalf("expected unknown site error")
	}
}
