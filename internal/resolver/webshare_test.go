package resolver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMapFileLinkErrorDocumentation(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{"FILE_LINK_FATAL_1", ErrFileNotFound}, {"FILE_LINK_FATAL_3", ErrWrongPassword},
		{"FILE_LINK_FATAL_4", ErrFileUnavailable}, {"FILE_LINK_FATAL_5", ErrTooManyDownloads},
		{"FILE_LINK_FATAL_6", ErrCopyrightedFile},
	}
	for _, test := range tests {
		if !errors.Is(mapFileLinkError(test.code, "message"), test.want) {
			t.Errorf("%s did not map to %v", test.code, test.want)
		}
	}
}

func TestWebshareClientSearchAndWST(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if form.Get("category") != "video" || form.Get("offset") != "20" || form.Get("wst") != "session-token" {
			t.Fatalf("unexpected form %q", form)
		}
		if req.Header.Get("X-WST") != "session-token" {
			t.Fatalf("missing WST header")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<response><status>OK</status><file><ident>abcde</ident><name>Show.S01E01.mkv</name><type>video</type><size>123</size><password>0</password></file></response>`))}, nil
	})}
	ws := NewWebshareClient(WebshareOptions{HTTPClient: client, BaseURL: "https://example.test/api", WST: "session-token"})
	got, err := ws.SearchVideos(context.Background(), "Show S01E01", 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Ident != "abcde" || got[0].Filename != "Show.S01E01.mkv" {
		t.Fatalf("unexpected result %#v", got)
	}
}

func TestWebshareResolverCanHandleOnlyWebshareHosts(t *testing.T) {
	r := NewWebshareResolver()
	if !r.CanHandle("https://webshare.cz/#/file/abcde/name") {
		t.Fatal("expected webshare host")
	}
	if !r.CanHandle("https://foo.webshare.cz/file/abcde") {
		t.Fatal("expected webshare subdomain")
	}
	if r.CanHandle("https://notwebshare.cz/file/abcde") {
		t.Fatal("unexpected host match")
	}
}

func TestExtractWebshareIdentRequiresWebshareHost(t *testing.T) {
	if got := ExtractWebshareIdent("https://webshare.cz/#/file/abcde/filename"); got != "abcde" {
		t.Fatalf("ident = %q", got)
	}
	if got := ExtractWebshareIdent("https://evil.example/file/abcde"); got != "" {
		t.Fatalf("accepted non-Webshare reference ident %q", got)
	}
	if got := ExtractWebshareIdent("https://webshare.cz/?ident=../bad"); got != "" {
		t.Fatalf("accepted invalid Webshare reference ident %q", got)
	}
}
