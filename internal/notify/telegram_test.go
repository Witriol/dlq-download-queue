package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendMessageRequestPathAndBody(t *testing.T) {
	var gotPath string
	var gotBody sendMessageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	if err := n.sendMessage(context.Background(), "secret-token", "chat-1", "hello"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	if gotPath != "/botsecret-token/sendMessage" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotBody.ChatID != "chat-1" || gotBody.Text != "hello" {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
}

func TestSendMessageRetriesOnceAfter429(t *testing.T) {
	var mu sync.Mutex
	attempts := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()

		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	start := time.Now()
	if err := n.sendMessage(context.Background(), "tok", "chat", "hi"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	elapsed := time.Since(start)

	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 2 {
		t.Fatalf("expected exactly 2 attempts (1 retry), got %d", got)
	}
	if elapsed < 1*time.Second {
		t.Fatalf("expected sendMessage to wait out retry_after, elapsed %v", elapsed)
	}
}

func TestSendMessageRetryAfterCappedAt60s(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"slow down","parameters":{"retry_after":120}}`))
	}))
	defer srv.Close()

	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := n.sendMessage(ctx, "tok", "chat", "hi")
	if err == nil {
		t.Fatalf("expected an error once ctx times out waiting for the capped retry_after")
	}
}

func TestSendMessageErrorNeverContainsToken(t *testing.T) {
	// Server returns a non-200, non-429 status with no listener for a
	// connection-refused case is simpler: point at a closed server so
	// the error comes from *url.Error, which embeds the request URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // immediately closed: connection is refused

	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	const token = "super-secret-token"
	err := n.sendMessage(context.Background(), token, "chat", "hi")
	if err == nil {
		t.Fatalf("expected an error from a closed server")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error must not contain the token: %v", err)
	}
}

func TestSendMessageErrorNeverContainsTokenWithTrailingSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed: connection refused, error comes from *url.Error

	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	const token = "super-secret-token "
	err := n.sendMessage(context.Background(), token, "chat", "hi")
	if err == nil {
		t.Fatalf("expected an error from a closed server")
	}
	msg := err.Error()
	if strings.Contains(msg, "super-secret-token") {
		t.Fatalf("error must not contain the token in raw, escaped or quoted form: %v", msg)
	}
	if strings.Contains(msg, "%20") {
		t.Fatalf("error must not contain the re-escaped URL: %v", msg)
	}
}

func TestSendMessageErrorNeverContainsTokenWithNewline(t *testing.T) {
	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = "http://127.0.0.1:1"

	const token = "super-secret-token\nX-Evil: 1"
	err := n.sendMessage(context.Background(), token, "chat", "hi")
	if err == nil {
		t.Fatalf("expected an error building/sending the request")
	}
	msg := err.Error()
	if strings.Contains(msg, "super-secret-token") {
		t.Fatalf("error must not contain the token in raw, escaped or quoted form: %v", msg)
	}
	if strings.Contains(msg, `\n`) {
		t.Fatalf("error must not contain the %%q-quoted token: %v", msg)
	}
}

func TestSendMessageErrorNeverContainsTokenOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()

	holder := &cfgHolder{}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	const token = "super-secret-token"
	err := n.sendMessage(context.Background(), token, "chat", "hi")
	if err == nil {
		t.Fatalf("expected an error for a 400 response")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error must not contain the token: %v", err)
	}
}
