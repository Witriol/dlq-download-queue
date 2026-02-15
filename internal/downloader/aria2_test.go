package downloader

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestIsGIDNotFoundMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "", want: false},
		{msg: "No such download", want: true},
		{msg: "GID cannot be found", want: true},
		{msg: "resource not found", want: true},
		{msg: "timeout while connecting", want: false},
	}
	for _, tt := range tests {
		if got := isGIDNotFoundMessage(tt.msg); got != tt.want {
			t.Fatalf("isGIDNotFoundMessage(%q)=%v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestIsActionNotAllowedMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "", want: false},
		{msg: "GID#abc cannot be paused now", want: true},
		{msg: "GID#abc cannot be unpaused now", want: true},
		{msg: "GID#abc cannot be resumed now", want: true},
		{msg: "GID#abc cannot be removed now", want: true},
		{msg: "No such download", want: false},
	}
	for _, tt := range tests {
		if got := isActionNotAllowedMessage(tt.msg); got != tt.want {
			t.Fatalf("isActionNotAllowedMessage(%q)=%v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestAria2RemoveFallsBackAndCleansResult(t *testing.T) {
	var methods []string
	client := NewAria2Client("http://aria2.local/jsonrpc", "")
	client.Client = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			defer r.Body.Close()
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			method, _ := req["method"].(string)
			methods = append(methods, method)

			payload := `{"jsonrpc":"2.0","id":"dlq","error":{"code":1,"message":"unexpected method"}}`
			switch method {
			case "aria2.remove":
				payload = `{"jsonrpc":"2.0","id":"dlq","error":{"code":1,"message":"GID#abc cannot be removed now"}}`
			case "aria2.forceRemove":
				payload = `{"jsonrpc":"2.0","id":"dlq","result":"abc"}`
			case "aria2.removeDownloadResult":
				payload = `{"jsonrpc":"2.0","id":"dlq","result":"OK"}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(payload)),
			}, nil
		}),
	}

	if err := client.Remove(context.Background(), "abc"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(methods) != 3 {
		t.Fatalf("expected 3 rpc calls, got %d (%v)", len(methods), methods)
	}
	if methods[0] != "aria2.remove" || methods[1] != "aria2.forceRemove" || methods[2] != "aria2.removeDownloadResult" {
		t.Fatalf("unexpected method order: %v", methods)
	}
}
