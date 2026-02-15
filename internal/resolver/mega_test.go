package resolver

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMegaResolverCanHandle(t *testing.T) {
	r := NewMegaResolver()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "mega nz", url: "https://mega.nz/file/abc#def", want: true},
		{name: "mega co nz", url: "https://mega.co.nz/#!abc!def", want: true},
		{name: "other host", url: "https://example.com/file/abc#def", want: false},
		{name: "invalid", url: "://bad", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := r.CanHandle(tt.url); got != tt.want {
				t.Fatalf("CanHandle(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseMegaFileLink(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  string
		wantKey string
		wantErr bool
	}{
		{
			name:    "new format",
			url:     "https://mega.nz/file/AbCdEf12#QwErTy123_-",
			wantID:  "AbCdEf12",
			wantKey: "QwErTy123_-",
		},
		{
			name:    "legacy format",
			url:     "https://mega.co.nz/#!AbCdEf12!QwErTy123_-",
			wantID:  "AbCdEf12",
			wantKey: "QwErTy123_-",
		},
		{
			name:    "folder link unsupported",
			url:     "https://mega.nz/folder/AbCdEf12#QwErTy123_-",
			wantErr: true,
		},
		{
			name:    "invalid token",
			url:     "https://mega.nz/file/AbCdEf12#bad+token",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotKey, err := parseMegaFileLink(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotID != tt.wantID || gotKey != tt.wantKey {
				t.Fatalf("got (%q,%q), want (%q,%q)", gotID, gotKey, tt.wantID, tt.wantKey)
			}
		})
	}
}

func TestMegaResolverResolveSuccess(t *testing.T) {
	rawKey := []byte{
		0x10, 0x11, 0x12, 0x13, 0x20, 0x21, 0x22, 0x23,
		0x30, 0x31, 0x32, 0x33, 0x40, 0x41, 0x42, 0x43,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}
	fileKey := base64.RawURLEncoding.EncodeToString(rawKey)
	attrs := encryptMegaAttributes(t, rawKey, "archive.zip")

	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if got := r.URL.Path; got != "/cs" {
				t.Fatalf("unexpected path: %s", got)
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"p":"AbCdEf12"`) {
				t.Fatalf("unexpected request body: %s", string(body))
			}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`[{"g":"https://dl.example.com/file.bin","s":12345,"at":"` + attrs + `"}]`)),
			}
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}),
	}

	r := &megaResolver{
		client: client,
		apiURL: "https://api.test/cs",
	}
	got, err := r.Resolve(context.Background(), "https://mega.nz/file/AbCdEf12#"+fileKey)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Kind != "aria2" {
		t.Fatalf("kind = %q, want aria2", got.Kind)
	}
	if got.URL != "https://dl.example.com/file.bin" {
		t.Fatalf("url = %q", got.URL)
	}
	if got.Filename != "archive.zip" {
		t.Fatalf("filename = %q", got.Filename)
	}
	if got.Size != 12345 {
		t.Fatalf("size = %d", got.Size)
	}
}

func TestMegaResolverResolveMapsQuotaError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`[-17]`)),
			}
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}),
	}

	r := &megaResolver{
		client: client,
		apiURL: "https://api.test/cs",
	}
	_, err := r.Resolve(context.Background(), "https://mega.nz/file/AbCdEf12#QwErTy123_-")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func encryptMegaAttributes(t *testing.T, rawKey []byte, filename string) string {
	t.Helper()
	if len(rawKey) != 32 {
		t.Fatalf("raw key length = %d, want 32", len(rawKey))
	}
	aesKey := make([]byte, 16)
	for i := 0; i < 16; i++ {
		aesKey[i] = rawKey[i] ^ rawKey[i+16]
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatalf("aes cipher: %v", err)
	}

	plain := []byte(`MEGA{"n":"` + filename + `"}`)
	if rem := len(plain) % aes.BlockSize; rem != 0 {
		plain = append(plain, make([]byte, aes.BlockSize-rem)...)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, plain)
	return base64.RawURLEncoding.EncodeToString(out)
}
