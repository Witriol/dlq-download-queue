package notify

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Default templates used when Config leaves the template empty.
const (
	defaultCompletedTemplate = "✅ {name} ({size}) finished in {duration}"
	defaultFailureTemplate   = "❌ {name}: {event} - {error}"
)

// timeFormat is the local-time layout used for {time}.
const timeFormat = "2006-01-02 15:04"

// collapseItems merges items that share a non-empty GroupKey and Event
// into one line (sizes summed, ids joined, earliest start, latest
// finish). Order of first appearance is preserved.
func collapseItems(items []Item) []Item {
	type groupKey struct {
		key   string
		event Event
	}

	result := make([]Item, 0, len(items))
	groups := make(map[groupKey][]Item)
	slot := make(map[groupKey]int)

	for _, it := range items {
		if it.GroupKey == "" {
			result = append(result, it)
			continue
		}

		gk := groupKey{it.GroupKey, it.Event}
		if _, ok := groups[gk]; !ok {
			slot[gk] = len(result)
			result = append(result, Item{}) // placeholder, filled below
		}
		groups[gk] = append(groups[gk], it)
	}

	for gk, group := range groups {
		result[slot[gk]] = mergeItems(group)
	}

	return result
}

func mergeItems(group []Item) Item {
	merged := group[0]
	if len(group) == 1 {
		return merged
	}

	var ids []int64
	var size int64
	var attempts, maxAttempts int
	var start, finish time.Time

	for _, it := range group {
		ids = append(ids, it.JobIDs...)
		size += it.SizeBytes

		if it.Attempts > attempts {
			attempts = it.Attempts
		}
		if it.MaxAttempts > maxAttempts {
			maxAttempts = it.MaxAttempts
		}
		if !it.StartedAt.IsZero() && (start.IsZero() || it.StartedAt.Before(start)) {
			start = it.StartedAt
		}
		if it.FinishedAt.After(finish) {
			finish = it.FinishedAt
		}
	}

	merged.JobIDs = ids
	merged.SizeBytes = size
	merged.Parts = len(group)
	merged.Attempts = attempts
	merged.MaxAttempts = maxAttempts
	merged.StartedAt = start
	merged.FinishedAt = finish

	return merged
}

// renderItem fills templateFor(it, cfg) with it's placeholders. Unknown
// {x} is left as-is; a known placeholder with no value renders empty.
func renderItem(it Item, cfg Config, info SeriesInfo) string {
	replacer := strings.NewReplacer(
		"{name}", resolveName(it),
		"{filename}", it.Filename,
		"{size}", humanBytes(it.SizeBytes),
		"{size_bytes}", strconv.FormatInt(it.SizeBytes, 10),
		"{time}", formatTime(it.FinishedAt),
		"{duration}", formatDuration(it.StartedAt, it.FinishedAt),
		"{speed}", formatSpeed(it.SizeBytes, it.StartedAt, it.FinishedAt),
		"{site}", it.Site,
		"{url}", it.URL,
		"{dir}", it.Dir,
		"{id}", joinIDs(it.JobIDs),
		"{parts}", strconv.Itoa(partsOrDefault(it.Parts)),
		"{status}", it.Status,
		"{event}", eventLabel(it.Event),
		"{error}", it.Error,
		"{error_code}", it.ErrorCode,
		"{attempts}", strconv.Itoa(it.Attempts),
		"{max_attempts}", strconv.Itoa(it.MaxAttempts),
		"{series}", info.Series,
		"{episode}", info.Episode,
		"{episode_title}", info.EpisodeTitle,
	)

	return replacer.Replace(templateFor(it, cfg))
}

func templateFor(it Item, cfg Config) string {
	if it.Event == EventCompleted {
		if cfg.CompletedTemplate != "" {
			return cfg.CompletedTemplate
		}
		return defaultCompletedTemplate
	}

	if cfg.FailureTemplate != "" {
		return cfg.FailureTemplate
	}
	return defaultFailureTemplate
}

func eventLabel(e Event) string {
	switch e {
	case EventCompleted:
		return "completed"
	case EventFailed:
		return "failed"
	case EventRetrying:
		return "retrying"
	case EventExtractFailed:
		return "extract failed"
	default:
		return string(e)
	}
}

// resolveName mirrors the display-name fallback used elsewhere
// (job Name, else Filename, else URL basename), except a grouped item
// (non-empty GroupKey) shows the archive group's label instead.
func resolveName(it Item) string {
	if it.GroupKey != "" {
		if label := groupLabelFromKey(it.GroupKey); label != "" {
			return label
		}
	}

	if it.Name != "" {
		return it.Name
	}
	if it.Filename != "" {
		return it.Filename
	}
	return basename(it.URL)
}

// groupLabelFromKey mirrors internal/queue's
// archiveGroupLabelFromKey: the group key is "part|part|label",
// pipe-separated, built by multipartArchiveGroupKey.
func groupLabelFromKey(groupKey string) string {
	parts := strings.Split(strings.TrimSpace(groupKey), "|")
	if len(parts) < 3 {
		return ""
	}
	return strings.TrimSpace(parts[2])
}

func basename(u string) string {
	trimmed := strings.TrimRight(u, "/")
	if trimmed == "" {
		return ""
	}

	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}

func joinIDs(ids []int64) string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(strs, ",")
}

// partsOrDefault treats an unset Parts (zero value) as a single job.
func partsOrDefault(p int) int {
	if p <= 0 {
		return 1
	}
	return p
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(time.Local).Format(timeFormat)
}

func formatDuration(start, finish time.Time) string {
	if start.IsZero() || finish.IsZero() {
		return ""
	}

	d := finish.Sub(start)
	if d < 0 {
		d = 0
	}
	return humanDuration(d)
}

func formatSpeed(size int64, start, finish time.Time) string {
	if start.IsZero() || finish.IsZero() {
		return ""
	}

	seconds := finish.Sub(start).Seconds()
	if seconds <= 0 {
		return ""
	}
	return humanBytes(int64(float64(size)/seconds)) + "/s"
}

// humanDuration mirrors cmd/dlq/format.go:humanDuration.
func humanDuration(d time.Duration) string {
	seconds := int64(d.Seconds())
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60

	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// humanBytes mirrors cmd/dlq/format.go:humanBytes.
func humanBytes(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	val := float64(n)
	idx := 0

	for val >= 1024 && idx < len(units)-1 {
		val /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f%s", val, units[idx])
}

// splitMessage splits text at line boundaries so each chunk stays within
// limit runes. A single line longer than limit is hard-split as a last
// resort.
func splitMessage(text string, limit int) []string {
	lines := strings.Split(text, "\n")
	var chunks []string
	var current []string
	length := 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(current, "\n"))
		current = nil
		length = 0
	}

	for _, line := range lines {
		runes := []rune(line)
		for len(runes) > limit {
			flush()
			chunks = append(chunks, string(runes[:limit]))
			runes = runes[limit:]
		}
		line = string(runes)

		sep := 0
		if len(current) > 0 {
			sep = 1
		}
		if length+sep+len(runes) > limit {
			flush()
			sep = 0
		}

		current = append(current, line)
		length += sep + len(runes)
	}
	flush()

	if len(chunks) == 0 {
		return []string{""}
	}
	return chunks
}

// sampleItem is the fixed item SendTest renders against.
func sampleItem() Item {
	now := time.Now()
	return Item{
		Event:      EventCompleted,
		JobIDs:     []int64{0},
		Name:       "Sample Job",
		Filename:   "sample.mkv",
		Site:       "example.com",
		URL:        "https://example.com/sample.mkv",
		Dir:        "/data/downloads",
		Status:     "completed",
		SizeBytes:  1_500_000_000,
		Parts:      1,
		StartedAt:  now.Add(-90 * time.Second),
		FinishedAt: now,
	}
}
