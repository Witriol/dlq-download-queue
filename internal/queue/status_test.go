package queue

import "testing"

func TestIsWebshareJob(t *testing.T) {
	tests := []struct {
		name   string
		site   string
		rawURL string
		want   bool
	}{
		{name: "site flag", site: "webshare", rawURL: "https://example.com/a", want: true},
		{name: "url host fallback", site: "", rawURL: "https://webshare.cz/#/file/abcde/test", want: true},
		{name: "mixed case host", site: "", rawURL: "https://WebShare.CZ/file/abcde", want: true},
		{name: "non webshare", site: "", rawURL: "https://mega.nz/file/abcde#key", want: false},
		{name: "empty", site: "", rawURL: "", want: false},
	}
	for _, tt := range tests {
		got := IsWebshareJob(tt.site, tt.rawURL)
		if got != tt.want {
			t.Fatalf("%s: IsWebshareJob(%q, %q)=%v want %v", tt.name, tt.site, tt.rawURL, got, tt.want)
		}
	}
}

func TestDisplayStatus(t *testing.T) {
	if got := DisplayStatus(StatusPaused, "webshare", ""); got != "stopped" {
		t.Fatalf("expected stopped for webshare paused status, got %q", got)
	}
	if got := DisplayStatus(StatusPaused, "", "https://webshare.cz/#/file/abcde/test"); got != "stopped" {
		t.Fatalf("expected stopped for webshare URL paused status, got %q", got)
	}
	if got := DisplayStatus(StatusPaused, "mega", "https://mega.nz/file/abcde#key"); got != StatusPaused {
		t.Fatalf("expected paused for non-webshare, got %q", got)
	}
	if got := DisplayStatus(StatusDownloading, "webshare", ""); got != StatusDownloading {
		t.Fatalf("expected unchanged status, got %q", got)
	}
}
