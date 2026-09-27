// Package notify batches job-completion and failure events and delivers
// them to Telegram.
package notify

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

// Event identifies a job lifecycle event that can trigger a notification.
type Event string

const (
	EventCompleted     Event = "completed"
	EventFailed        Event = "failed"
	EventRetrying      Event = "retrying"
	EventExtractFailed Event = "extract_failed"
)

// Item describes a single job event to notify about.
type Item struct {
	Event    Event
	JobIDs   []int64
	GroupKey string // multipart group, "" otherwise

	Name     string
	Filename string
	Site     string
	URL      string
	Dir      string

	Status    string
	Error     string
	ErrorCode string

	SizeBytes int64
	Parts     int

	Attempts    int
	MaxAttempts int

	StartedAt  time.Time // zero = unknown
	FinishedAt time.Time // zero = unknown

	SourceKey string
}

// Config is the live Telegram notification configuration, re-read at
// enqueue time and again at flush time.
type Config struct {
	Enabled           bool
	BotToken          string
	ChatID            string
	Events            map[Event]bool
	CompletedTemplate string
	FailureTemplate   string
}

// SeriesInfo carries series/episode display fields for a job's SourceKey.
type SeriesInfo struct {
	Series       string
	Episode      string
	EpisodeTitle string
}

// enqueueBufferSize bounds the pending-item channel; a full buffer means
// Enqueue drops the item rather than block the caller.
const enqueueBufferSize = 256

// defaultBatchWindow is the delay between the first item of a batch and
// the flush that sends it.
const defaultBatchWindow = 30 * time.Second

// Notifier batches Items and sends one Telegram message per window.
type Notifier struct {
	cfg    func() Config
	lookup func(ctx context.Context, sourceKey string) (SeriesInfo, bool)

	items chan Item

	batchWindow time.Duration // private so tests avoid a 30s sleep
	botAPIBase  string        // private so tests can point at httptest.Server
	httpClient  *http.Client
}

// NewNotifier builds a Notifier. lookup may be nil.
func NewNotifier(cfg func() Config, lookup func(ctx context.Context, sourceKey string) (SeriesInfo, bool)) *Notifier {
	return &Notifier{
		cfg:         cfg,
		lookup:      lookup,
		items:       make(chan Item, enqueueBufferSize),
		batchWindow: defaultBatchWindow,
		botAPIBase:  telegramAPIBase,
		httpClient:  &http.Client{Timeout: httpTimeout},
	}
}

// Enqueue queues item for the current or next batch window. It never
// blocks: the item is dropped if notifications are disabled, the event
// is toggled off, or the internal buffer is full.
func (n *Notifier) Enqueue(item Item) {
	cfg := n.cfg()
	if !cfg.Enabled || !cfg.Events[item.Event] {
		return
	}

	select {
	case n.items <- item:
	default: // buffer full: drop rather than block the caller
	}
}

// Run drives the batch loop until ctx is done.
func (n *Notifier) Run(ctx context.Context) {
	var pending []Item
	var timer *time.Timer
	var timerC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return

		case item := <-n.items:
			pending = append(pending, item)
			if timer == nil {
				timer = time.NewTimer(n.batchWindow)
				timerC = timer.C
			}

		case <-timerC:
			n.flush(ctx, pending)
			pending = nil
			timer = nil
			timerC = nil
		}
	}
}

// flush renders and sends one batch. Config is re-read here: if
// notifications were disabled, or an event toggled off, during the
// window, the affected items are dropped instead of sent.
func (n *Notifier) flush(ctx context.Context, items []Item) {
	if len(items) == 0 {
		return
	}

	cfg := n.cfg()
	if !cfg.Enabled {
		return
	}

	live := make([]Item, 0, len(items))
	for _, it := range items {
		if cfg.Events[it.Event] {
			live = append(live, it)
		}
	}
	if len(live) == 0 {
		return
	}

	lines := make([]string, 0, len(live))
	for _, it := range collapseItems(live) {
		// A template rendering to only whitespace (e.g. "{error}" with no
		// error) would send Telegram an empty message, which it rejects.
		if line := renderItem(it, cfg, n.seriesInfo(ctx, it.SourceKey)); strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return
	}

	text := strings.Join(lines, "\n")
	for _, chunk := range splitMessage(text, telegramMessageLimit) {
		if err := n.sendMessage(ctx, cfg.BotToken, cfg.ChatID, chunk); err != nil {
			log.Printf("notify: telegram send failed: %v", err)
		}
	}
}

func (n *Notifier) seriesInfo(ctx context.Context, sourceKey string) SeriesInfo {
	if n.lookup == nil || sourceKey == "" {
		return SeriesInfo{}
	}

	info, ok := n.lookup(ctx, sourceKey)
	if !ok {
		return SeriesInfo{}
	}

	return info
}

// SendTest renders template against a fixed sample item and sends it
// immediately: no batching, and it works even while cfg is disabled. An
// empty token or chatID falls back to the configured values.
func (n *Notifier) SendTest(ctx context.Context, token, chatID, template string) error {
	cfg := n.cfg()
	if token == "" {
		token = cfg.BotToken
	}
	if chatID == "" {
		chatID = cfg.ChatID
	}

	text := renderItem(sampleItem(), Config{CompletedTemplate: template}, SeriesInfo{})
	if strings.TrimSpace(text) == "" {
		return errors.New("notify: template rendered an empty message")
	}
	return n.sendMessage(ctx, token, chatID, text)
}
