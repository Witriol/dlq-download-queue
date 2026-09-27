package notify

import (
	"strings"
	"testing"
	"time"
)

func TestRenderItemAllPlaceholders(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	finish := start.Add(90 * time.Second)
	it := Item{
		Event:       EventCompleted,
		JobIDs:      []int64{1, 2},
		Name:        "Movie",
		Filename:    "movie.mkv",
		Site:        "example.com",
		URL:         "https://example.com/movie.mkv",
		Dir:         "/data/movies",
		Status:      "completed",
		Error:       "boom",
		ErrorCode:   "E1",
		SizeBytes:   1024,
		Parts:       2,
		Attempts:    1,
		MaxAttempts: 3,
		StartedAt:   start,
		FinishedAt:  finish,
	}
	cfg := Config{CompletedTemplate: strings.Join([]string{
		"{name}", "{filename}", "{size}", "{size_bytes}", "{time}", "{duration}",
		"{speed}", "{site}", "{url}", "{dir}", "{id}", "{parts}", "{status}",
		"{event}", "{error}", "{error_code}", "{attempts}", "{max_attempts}",
		"{series}", "{episode}", "{episode_title}",
	}, "|")}
	info := SeriesInfo{Series: "Show", Episode: "S01E02", EpisodeTitle: "Pilot"}

	sizeBytes := int64(1024)
	durationSeconds := 90.0
	wantSpeed := humanBytes(int64(float64(sizeBytes)/durationSeconds)) + "/s"

	got := renderItem(it, cfg, info)
	want := strings.Join([]string{
		"Movie", "movie.mkv", humanBytes(1024), "1024",
		finish.In(time.Local).Format(timeFormat),
		humanDuration(90 * time.Second),
		wantSpeed,
		"example.com", "https://example.com/movie.mkv", "/data/movies",
		"1,2", "2", "completed", "completed", "boom", "E1", "1", "3",
		"Show", "S01E02", "Pilot",
	}, "|")

	if got != want {
		t.Fatalf("render mismatch:\n got=%s\nwant=%s", got, want)
	}
}

func TestRenderItemUnknownPlaceholderKeptEmptyRendersBlank(t *testing.T) {
	it := Item{Event: EventFailed}
	cfg := Config{FailureTemplate: "{name}|{unknown}|{time}|{duration}|{speed}|{series}"}

	got := renderItem(it, cfg, SeriesInfo{})
	want := "|{unknown}||||"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderItemDefaultTemplates(t *testing.T) {
	completed := Item{Event: EventCompleted, Name: "a", SizeBytes: 1024, StartedAt: time.Now().Add(-1 * time.Second), FinishedAt: time.Now()}
	got := renderItem(completed, Config{}, SeriesInfo{})
	if !strings.Contains(got, "a") || !strings.HasPrefix(got, "✅") {
		t.Fatalf("expected default completed template applied, got %q", got)
	}

	failed := Item{Event: EventFailed, Name: "b", Error: "oops"}
	got = renderItem(failed, Config{}, SeriesInfo{})
	if !strings.Contains(got, "b") || !strings.Contains(got, "oops") || !strings.HasPrefix(got, "❌") {
		t.Fatalf("expected default failure template applied, got %q", got)
	}
}

func TestRenderItemNameFallback(t *testing.T) {
	cases := []struct {
		it   Item
		want string
	}{
		{Item{Name: "n", Filename: "f", URL: "https://x/y.bin"}, "n"},
		{Item{Filename: "f", URL: "https://x/y.bin"}, "f"},
		{Item{URL: "https://x/y.bin"}, "y.bin"},
		{Item{}, ""},
	}
	for _, c := range cases {
		if got := resolveName(c.it); got != c.want {
			t.Fatalf("resolveName(%+v) = %q, want %q", c.it, got, c.want)
		}
	}
}

func TestGroupLabelFromKeyOverridesName(t *testing.T) {
	it := Item{Name: "ignored", GroupKey: "a|b|Movie Set"}
	if got := resolveName(it); got != "Movie Set" {
		t.Fatalf("got %q, want %q", got, "Movie Set")
	}

	// malformed key (fewer than 3 parts) falls back to job fields.
	it2 := Item{Name: "fallback", GroupKey: "only-one-part"}
	if got := resolveName(it2); got != "fallback" {
		t.Fatalf("got %q, want %q", got, "fallback")
	}
}

func TestCollapseItemsMergesSameGroupAndEvent(t *testing.T) {
	start1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	start2 := start1.Add(-5 * time.Minute)
	finish1 := start1.Add(10 * time.Minute)
	finish2 := finish1.Add(5 * time.Minute)

	items := []Item{
		{Event: EventCompleted, GroupKey: "g|1|Set", JobIDs: []int64{1}, SizeBytes: 100, StartedAt: start1, FinishedAt: finish1},
		{Event: EventFailed, GroupKey: "g|1|Set", JobIDs: []int64{9}, SizeBytes: 999}, // different event: not merged
		{Event: EventCompleted, GroupKey: "g|1|Set", JobIDs: []int64{2}, SizeBytes: 200, StartedAt: start2, FinishedAt: finish2},
		{Event: EventCompleted, GroupKey: "", JobIDs: []int64{3}, SizeBytes: 50}, // ungrouped: passes through
	}

	got := collapseItems(items)
	if len(got) != 3 {
		t.Fatalf("expected 3 result items, got %d", len(got))
	}

	merged := got[0]
	if merged.SizeBytes != 300 {
		t.Fatalf("expected summed size 300, got %d", merged.SizeBytes)
	}
	if merged.Parts != 2 {
		t.Fatalf("expected parts=2, got %d", merged.Parts)
	}
	if joinIDs(merged.JobIDs) != "1,2" {
		t.Fatalf("expected joined ids 1,2, got %s", joinIDs(merged.JobIDs))
	}
	if !merged.StartedAt.Equal(start2) {
		t.Fatalf("expected earliest start %v, got %v", start2, merged.StartedAt)
	}
	if !merged.FinishedAt.Equal(finish2) {
		t.Fatalf("expected latest finish %v, got %v", finish2, merged.FinishedAt)
	}

	if got[1].Event != EventFailed || got[1].SizeBytes != 999 {
		t.Fatalf("expected the different-event item untouched, got %+v", got[1])
	}
	if got[2].SizeBytes != 50 || got[2].GroupKey != "" {
		t.Fatalf("expected the ungrouped item untouched, got %+v", got[2])
	}
}

func TestSplitMessageSplitsAtLineBoundaries(t *testing.T) {
	line := strings.Repeat("x", 100)
	lines := make([]string, 50) // 50 * 101 > 4096
	for i := range lines {
		lines[i] = line
	}
	text := strings.Join(lines, "\n")

	chunks := splitMessage(text, telegramMessageLimit)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len([]rune(c)) > telegramMessageLimit {
			t.Fatalf("chunk exceeds limit: %d runes", len([]rune(c)))
		}
	}
	if strings.Join(chunks, "\n") != text {
		t.Fatalf("chunks do not reconstruct the original text")
	}
}

func TestSplitMessageUnderLimitIsOneChunk(t *testing.T) {
	text := "line one\nline two"
	chunks := splitMessage(text, telegramMessageLimit)
	if len(chunks) != 1 || chunks[0] != text {
		t.Fatalf("expected single chunk %q, got %v", text, chunks)
	}
}
