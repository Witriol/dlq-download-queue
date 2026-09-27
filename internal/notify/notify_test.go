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

// cfgHolder lets tests mutate the Config a Notifier reads, simulating
// settings changing while a batch window is open.
type cfgHolder struct {
	mu  sync.Mutex
	cfg Config
}

func (h *cfgHolder) get() Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg
}

func (h *cfgHolder) set(cfg Config) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg = cfg
}

// recordingServer captures every request body sent to it.
func recordingServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var bodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(bodies))
		copy(out, bodies)
		return out
	}
}

func TestNotifierBatchesWithinWindowIntoOneSend(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		Enabled:  true,
		BotToken: "tok",
		ChatID:   "chat",
		Events:   map[Event]bool{EventCompleted: true},
	}}

	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL
	n.batchWindow = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Enqueue(Item{Event: EventCompleted, Name: "first"})
	n.Enqueue(Item{Event: EventCompleted, Name: "second"})

	time.Sleep(100 * time.Millisecond)

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "first") || !strings.Contains(got[0], "second") {
		t.Fatalf("expected both items in one message, got %s", got[0])
	}
}

func TestNotifierCollapsesByGroupKeyAndEvent(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		Enabled:  true,
		BotToken: "tok",
		ChatID:   "chat",
		Events:   map[Event]bool{EventCompleted: true},
	}}

	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL
	n.batchWindow = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Enqueue(Item{Event: EventCompleted, GroupKey: "g|1|Boxset", JobIDs: []int64{1}, SizeBytes: 100})
	n.Enqueue(Item{Event: EventCompleted, GroupKey: "g|1|Boxset", JobIDs: []int64{2}, SizeBytes: 200})

	time.Sleep(100 * time.Millisecond)

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d: %v", len(got), got)
	}
	if strings.Count(got[0], "Boxset") != 1 {
		t.Fatalf("expected the two items collapsed into a single Boxset line, got %s", got[0])
	}
}

func TestNotifierSplitsOversizedBatch(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		Enabled:           true,
		BotToken:          "tok",
		ChatID:            "chat",
		Events:            map[Event]bool{EventCompleted: true},
		CompletedTemplate: "{name}",
	}}

	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL
	n.batchWindow = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	longName := strings.Repeat("n", 200)
	for i := 0; i < 30; i++ { // 30 * 201 > 4096
		n.Enqueue(Item{Event: EventCompleted, Name: longName})
	}

	time.Sleep(150 * time.Millisecond)

	got := bodies()
	if len(got) < 2 {
		t.Fatalf("expected the batch split across multiple messages, got %d: %v", len(got), got)
	}
}

func TestNotifierDisabledAtFlushDropsBatch(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		Enabled:  true,
		BotToken: "tok",
		ChatID:   "chat",
		Events:   map[Event]bool{EventCompleted: true},
	}}

	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL
	n.batchWindow = 30 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Enqueue(Item{Event: EventCompleted, Name: "first"})

	// Disable before the window elapses: flush must drop the batch.
	holder.set(Config{Enabled: false, Events: map[Event]bool{EventCompleted: true}})

	time.Sleep(100 * time.Millisecond)

	got := bodies()
	if len(got) != 0 {
		t.Fatalf("expected no requests, got %d: %v", len(got), got)
	}
}

func TestNotifierEnqueueDropsWhenDisabledOrEventOff(t *testing.T) {
	holder := &cfgHolder{cfg: Config{
		Enabled: true,
		Events:  map[Event]bool{EventCompleted: true},
	}}
	n := NewNotifier(holder.get, nil)

	holder.set(Config{Enabled: false, Events: map[Event]bool{EventCompleted: true}})
	n.Enqueue(Item{Event: EventCompleted})

	holder.set(Config{Enabled: true, Events: map[Event]bool{EventCompleted: false}})
	n.Enqueue(Item{Event: EventCompleted})

	select {
	case <-n.items:
		t.Fatalf("expected item to be dropped, but one was queued")
	default:
	}
}

func TestNotifierSkipsEmptyRenderedLines(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		Enabled:  true,
		BotToken: "tok",
		ChatID:   "chat",
		Events:   map[Event]bool{EventCompleted: true, EventFailed: true},
		// "{error}" is empty for a completed item, so the rendered line
		// for the first item is empty; only the second must be sent.
		CompletedTemplate: "{error}",
		FailureTemplate:   "{name}",
	}}

	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL
	n.batchWindow = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Enqueue(Item{Event: EventCompleted, Name: "empty-line"})
	n.Enqueue(Item{Event: EventFailed, Name: "real-line"})

	time.Sleep(100 * time.Millisecond)

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d: %v", len(got), got)
	}
	var body sendMessageRequest
	if err := json.Unmarshal([]byte(got[0]), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Text != "real-line" {
		t.Fatalf("expected only the non-empty line, got %q", body.Text)
	}
}

func TestNotifierDropsBatchWhenEveryLineIsEmpty(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		Enabled:           true,
		BotToken:          "tok",
		ChatID:            "chat",
		Events:            map[Event]bool{EventCompleted: true},
		CompletedTemplate: "{error}", // always empty for a completed item
	}}

	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL
	n.batchWindow = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Enqueue(Item{Event: EventCompleted, Name: "a"})

	time.Sleep(100 * time.Millisecond)

	got := bodies()
	if len(got) != 0 {
		t.Fatalf("expected no requests when every rendered line is empty, got %d: %v", len(got), got)
	}
}

func TestSendTestRejectsEmptyRenderedMessage(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{BotToken: "stored-token", ChatID: "stored-chat"}}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	err := n.SendTest(context.Background(), "", "", "{error}") // empty for the sample (completed) item
	if err == nil {
		t.Fatalf("expected an error for an empty rendered test message")
	}
	if len(bodies()) != 0 {
		t.Fatalf("expected no request sent for an empty message, got %d", len(bodies()))
	}
}

func TestSendTestFallsBackToConfiguredTokenAndChatID(t *testing.T) {
	srv, bodies := recordingServer(t)
	defer srv.Close()

	holder := &cfgHolder{cfg: Config{
		BotToken: "stored-token",
		ChatID:   "stored-chat",
	}}
	n := NewNotifier(holder.get, nil)
	n.botAPIBase = srv.URL

	if err := n.SendTest(context.Background(), "", "", "{name}"); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	if !strings.Contains(got[0], "Sample Job") {
		t.Fatalf("expected sample item name in body, got %s", got[0])
	}
}
