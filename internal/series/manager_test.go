package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Witriol/dlq-download-queue/internal/queue"
	"github.com/Witriol/dlq-download-queue/internal/resolver"
)

type fakeTVMaze struct {
	episodes     []TVMazeEpisode
	calls        int
	showStatus   string
	showTimezone string
	showCalls    int
}

type blockingTVMaze struct {
	started chan struct{}
	release chan struct{}
}

type parallelTVMaze struct {
	started chan struct{}
	release chan struct{}
}

func (f *parallelTVMaze) SearchShows(context.Context, string) ([]TVMazeSearchResult, error) {
	return nil, nil
}

func (f *parallelTVMaze) Episodes(context.Context, int64) ([]TVMazeEpisode, error) {
	f.started <- struct{}{}
	<-f.release
	return nil, nil
}

func (f *parallelTVMaze) Show(context.Context, int64) (*TVMazeShow, error) {
	return &TVMazeShow{}, nil
}

func (f *blockingTVMaze) SearchShows(context.Context, string) ([]TVMazeSearchResult, error) {
	return nil, nil
}

func (f *blockingTVMaze) Episodes(context.Context, int64) ([]TVMazeEpisode, error) {
	close(f.started)
	<-f.release
	return nil, nil
}

func (f *blockingTVMaze) Show(context.Context, int64) (*TVMazeShow, error) {
	return &TVMazeShow{}, nil
}

func (f *fakeTVMaze) SearchShows(context.Context, string) ([]TVMazeSearchResult, error) {
	return nil, nil
}

func (f *fakeTVMaze) Episodes(context.Context, int64) ([]TVMazeEpisode, error) {
	f.calls++
	return f.episodes, nil
}

func (f *fakeTVMaze) Show(_ context.Context, id int64) (*TVMazeShow, error) {
	f.showCalls++
	// A global web channel has a null country.
	channel := &tvMazeChannel{Name: "Web"}
	if f.showTimezone != "" {
		channel.Country = &tvMazeCountry{Timezone: f.showTimezone}
	}
	return &TVMazeShow{ID: id, Status: f.showStatus, WebChannel: channel}, nil
}

type fakeWebshare struct {
	results []resolver.WebshareSearchResult
}

func (f *fakeWebshare) SearchVideos(context.Context, string, int, int) ([]resolver.WebshareSearchResult, error) {
	return f.results, nil
}

func (f *fakeWebshare) FileInfo(context.Context, string) (*resolver.WebshareFile, error) {
	return nil, errors.New("unexpected FileInfo call")
}

type fakeQueue struct {
	db       *sql.DB
	failures int
	calls    int
	outDirs  []string
}

func (f *fakeQueue) CreateJob(ctx context.Context, rawURL, outDir, name, site, archivePassword string, maxAttempts int) (int64, error) {
	f.calls++
	f.outDirs = append(f.outDirs, outDir)
	if f.failures > 0 {
		f.failures--
		return 0, errors.New("temporary queue failure")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := f.db.ExecContext(ctx, `INSERT INTO jobs (url, out_dir, status, created_at, updated_at) VALUES (?, ?, 'queued', ?, ?)`, rawURL, outDir, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func newManagerFixture(t *testing.T, policy string, candidateName string, queueFailures int) (*Manager, *Store, *fakeQueue, int64) {
	t.Helper()
	store, conn := newSeriesStore(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	number := 2
	air := now.Add(-48 * time.Hour)
	profile := ParseReleaseName("Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv")
	encoded, err := json.Marshal(storedProfile{Profile: profile, Preferences: map[string]string{
		"resolution": PreferenceRequire, "codec": PreferencePrefer, "source": PreferencePrefer,
		"release_group": PreferencePrefer, "container": PreferenceIgnore,
	}})
	if err != nil {
		t.Fatal(err)
	}
	watch := &Watch{
		Enabled: true, TVMazeID: 42, DisplayName: "Some Show", SearchTitle: "Some Show",
		ReferenceWebshareIdent: "reference", ReferenceFilename: profile.Filename,
		OutDir: "/data/tv", QualityProfileJSON: string(encoded), StartMode: StartModeSpecific,
		StartSeason: sql.NullInt64{Int64: 1, Valid: true}, StartEpisode: sql.NullInt64{Int64: 2, Valid: true},
		FallbackPolicy: policy, PreferredWaitSeconds: 1,
	}
	watchID, err := store.CreateWatch(context.Background(), watch)
	if err != nil {
		t.Fatal(err)
	}
	queue := &fakeQueue{db: conn, failures: queueFailures}
	manager := &Manager{
		Store:  store,
		TVMaze: &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}},
		Webshare: &fakeWebshare{results: []resolver.WebshareSearchResult{{
			Ident: "candidate", Filename: candidateName, Size: 700 << 20, Available: true,
		}}},
		Queue: queue, AllowedRoots: []string{"/data"}, Now: func() time.Time { return now },
	}
	return manager, store, queue, watchID
}

// exhaustSearchWindow searches at the window start and again at give-up.
func exhaustSearchWindow(t *testing.T, manager *Manager, watchID int64) {
	t.Helper()
	start := manager.now()
	for _, at := range []time.Time{start, start.Add(searchGiveUpAfter)} {
		manager.Now = func() time.Time { return at }
		if err := manager.processWatch(context.Background(), watchID); err != nil {
			t.Fatal(err)
		}
	}
}

func setPreferredWait(t *testing.T, store *Store, watchID int64, wait time.Duration) {
	t.Helper()
	watch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	watch.PreferredWaitSeconds = int64(wait / time.Second)
	if err := store.UpdateWatch(context.Background(), watch); err != nil {
		t.Fatal(err)
	}
}

func watchNextCheck(t *testing.T, store *Store, watchID int64) time.Time {
	t.Helper()
	watch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	next, ok := parseNullTime(watch.NextCheckAt)
	if !ok {
		t.Fatalf("next_check_at = %+v", watch.NextCheckAt)
	}
	return next
}

func latestEventMessage(t *testing.T, store *Store, watchID int64) string {
	t.Helper()
	events, err := store.ListEvents(context.Background(), watchID, 1)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	return events[0].Message
}

func TestManagerFallbackPolicies(t *testing.T) {
	const alternative = "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv"
	tests := []struct {
		policy    string
		wantCalls int
		wantState string
	}{
		{policy: FallbackStrict, wantCalls: 0, wantState: StatePreferredNotFound},
		{policy: FallbackBalanced, wantCalls: 1, wantState: StateQueued},
		{policy: FallbackManual, wantCalls: 0, wantState: StateNeedsAttention},
	}
	for _, test := range tests {
		t.Run(test.policy, func(t *testing.T) {
			manager, store, queue, watchID := newManagerFixture(t, test.policy, alternative, 0)
			start := manager.now()
			for _, at := range []time.Time{start, start.Add(time.Second)} {
				manager.Now = func() time.Time { return at }
				if err := manager.processWatch(context.Background(), watchID); err != nil {
					t.Fatal(err)
				}
			}
			episodes, err := store.ListEpisodes(context.Background(), watchID)
			if err != nil || len(episodes) != 1 {
				t.Fatalf("episodes = %#v, err = %v", episodes, err)
			}
			if queue.calls != test.wantCalls || episodes[0].State != test.wantState {
				t.Fatalf("calls = %d, state = %q; want %d, %q", queue.calls, episodes[0].State, test.wantCalls, test.wantState)
			}
		})
	}
}

func TestManagerSearchTiersByWindowAge(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	start := manager.now()
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got, want := latestEventMessage(t, store, watchID), "S01E02 search #1: 1 results, 1 accepted, 0 exact; next search 2026-09-18 12:15 UTC"; got != want {
		t.Fatalf("event = %q; want %q", got, want)
	}
	tests := []struct {
		age, interval time.Duration
	}{
		{age: 10 * time.Minute, interval: 15 * time.Minute},
		{age: 7 * time.Hour, interval: time.Hour},
		{age: 30 * time.Hour, interval: 3 * time.Hour},
		{age: 70 * time.Hour, interval: 2 * time.Hour},
	}
	for _, test := range tests {
		now := start.Add(test.age)
		manager.Now = func() time.Time { return now }
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
		if got, want := watchNextCheck(t, store, watchID), now.Add(test.interval); !got.Equal(want) {
			t.Fatalf("age %v next check = %v; want %v", test.age, got, want)
		}
	}
}

func TestManagerGivesUpAtSearchWindowEnd(t *testing.T) {
	const alternative = "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv"
	const wrongEpisode = "Some.Show.S01E03.1080p.WEB-DL.x265-MeGusta.mkv"
	tests := []struct {
		policy        string
		candidate     string
		wantAttention int
	}{
		{policy: FallbackStrict, candidate: alternative, wantAttention: -1},
		{policy: FallbackBalanced, candidate: wrongEpisode, wantAttention: -1},
		// A preferred wait beyond the window leaves manual review to give-up.
		{policy: FallbackManual, candidate: alternative, wantAttention: 1},
	}
	for _, test := range tests {
		t.Run(test.policy, func(t *testing.T) {
			manager, store, queue, watchID := newManagerFixture(t, test.policy, test.candidate, 0)
			ctx := context.Background()
			setPreferredWait(t, store, watchID, 100*time.Hour)
			start := manager.now()
			for _, at := range []time.Time{start, start.Add(searchGiveUpAfter - time.Minute)} {
				manager.Now = func() time.Time { return at }
				if err := manager.processWatch(ctx, watchID); err != nil {
					t.Fatal(err)
				}
			}
			episodes, err := store.ListEpisodes(ctx, watchID)
			if err != nil || episodes[0].State != StatePreferredNotFound {
				t.Fatalf("episode before give-up = %+v, err = %v", episodes, err)
			}
			manager.Now = func() time.Time { return start.Add(searchGiveUpAfter) }
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			episodes, err = store.ListEpisodes(ctx, watchID)
			if err != nil || episodes[0].State != StateNeedsAttention || queue.calls != 0 {
				t.Fatalf("episode after give-up = %+v, calls = %d, err = %v", episodes, queue.calls, err)
			}
			var snapshot []CandidateView
			if episodes[0].AttentionCandidatesJSON.Valid {
				if err := json.Unmarshal([]byte(episodes[0].AttentionCandidatesJSON.String), &snapshot); err != nil {
					t.Fatal(err)
				}
			} else {
				snapshot = nil
			}
			if test.wantAttention < 0 && episodes[0].AttentionCandidatesJSON.Valid {
				t.Fatalf("automatic policy stored attention snapshot %s", episodes[0].AttentionCandidatesJSON.String)
			}
			if test.wantAttention >= 0 && len(snapshot) != test.wantAttention {
				t.Fatalf("manual snapshot = %+v; want %d candidates", snapshot, test.wantAttention)
			}
			message := latestEventMessage(t, store, watchID)
			if !strings.HasPrefix(message, "S01E02 gave up after 72h: ") {
				t.Fatalf("give-up event = %q", message)
			}
		})
	}
}

func TestManagerBalancedFallbackFiresAtPreferredWait(t *testing.T) {
	manager, store, queue, watchID := newManagerFixture(t, FallbackBalanced, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	setPreferredWait(t, store, watchID, 12*time.Hour)
	start := manager.now()
	fallbackAt := start.Add(12 * time.Hour)
	for _, at := range []time.Time{start, fallbackAt.Add(-30 * time.Minute)} {
		manager.Now = func() time.Time { return at }
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
	if queue.calls != 0 {
		t.Fatalf("alternative queued before preferred wait: calls=%d", queue.calls)
	}
	// The 1 h tier would search at +12h30m; the fallback instant wins.
	if got := watchNextCheck(t, store, watchID); !got.Equal(fallbackAt) {
		t.Fatalf("next check = %v; want fallback instant %v", got, fallbackAt)
	}
	manager.Now = func() time.Time { return fallbackAt }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || queue.calls != 1 || episodes[0].State != StateQueued {
		t.Fatalf("fallback did not queue: calls=%d episodes=%+v err=%v", queue.calls, episodes, err)
	}
	if got, want := latestEventMessage(t, store, watchID), "S01E02 queued job #1 (alternative after 12h): Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv"; got != want {
		t.Fatalf("event = %q; want %q", got, want)
	}

	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateDownloading}, nil }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, watchID, 2)
	if err != nil || len(events) != 2 || events[1].Message != "S01E02 job #1 downloading" {
		t.Fatalf("transition events = %+v, err = %v", events, err)
	}
}

func TestManagerManualReviewAtPreferredWait(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackManual, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	setPreferredWait(t, store, watchID, 12*time.Hour)
	start := manager.now()
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := watchNextCheck(t, store, watchID); !got.Equal(start.Add(15 * time.Minute)) {
		t.Fatalf("next check = %v", got)
	}
	manager.Now = func() time.Time { return start.Add(12 * time.Hour) }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	attention, err := manager.ListAttention(ctx, watchID)
	if err != nil || len(attention) != 1 || len(attention[0].Candidates) != 1 {
		t.Fatalf("attention = %+v, err = %v", attention, err)
	}
	if got, want := latestEventMessage(t, store, watchID), "S01E02 needs review: 1 alternatives"; got != want {
		t.Fatalf("event = %q; want %q", got, want)
	}
}

func TestManagerBacklogEpisodeGetsFullWindow(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	now := manager.now()
	number := 2
	air := now.Add(-30 * 24 * time.Hour)
	manager.TVMaze = &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes = %+v, err = %v", episodes, err)
	}
	started, ok := parseNullTime(episodes[0].SearchStartedAt)
	if episodes[0].State != StatePreferredNotFound || !ok || !started.Equal(now) {
		t.Fatalf("backlog episode = %+v", episodes[0])
	}
}

func TestManagerPrunesEventsPerWatch(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1005)
INSERT INTO watch_events (watch_id, level, message, created_at)
SELECT ?, 'info', 'old ' || i, '2026-01-01T00:00:00Z' FROM n`, watchID); err != nil {
		t.Fatal(err)
	}
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, watchID, 0)
	if err != nil || len(events) != maxEventsPerWatch {
		t.Fatalf("events after prune = %d, err = %v", len(events), err)
	}
	// The first check also logs "tracking 1 episode from TVmaze", one more
	// event than before, so one more old event is pruned than would otherwise
	// be kept.
	if !strings.HasPrefix(events[0].Message, "S01E02 queued job #1 (exact)") || events[len(events)-1].Message != "old 8" {
		t.Fatalf("prune kept wrong events: newest=%q oldest=%q", events[0].Message, events[len(events)-1].Message)
	}
}

func TestManagerCheckNowResetsExhaustedAutomaticEpisode(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	exhaustSearchWindow(t, manager, watchID)
	before, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(before) != 1 || before[0].State != StateNeedsAttention {
		t.Fatalf("episode before recovery = %#v, err = %v", before, err)
	}
	if _, err := manager.CheckNow(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(after) != 1 {
		t.Fatalf("episodes after recovery = %#v, err = %v", after, err)
	}
	if after[0].State != StatePreferredNotFound || after[0].SearchAttempts != 1 {
		t.Fatalf("manual recovery did not begin a fresh cycle: %+v", after[0])
	}
	if got := countEvents(t, store, watchID, "manual check requested"); got != 1 {
		t.Fatalf("manual check requested events = %d; want 1", got)
	}
}

func TestManagerUpdateResetsExhaustedAutomaticEpisode(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	exhaustSearchWindow(t, manager, watchID)
	updatedTitle := "Some Show (2026)"
	if _, err := manager.Update(ctx, watchID, UpdateRequest{SearchTitle: &updatedTitle}); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes after update = %#v, err = %v", episodes, err)
	}
	if episodes[0].State != StateScheduled || episodes[0].SearchAttempts != 0 || episodes[0].AttentionCandidatesJSON.Valid || episodes[0].SearchStartedAt.Valid {
		t.Fatalf("settings update did not reset exhausted episode: %+v", episodes[0])
	}
	// The reset restarts the window: the next search is a first search again.
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	episodes, err = store.ListEpisodes(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	started, ok := parseNullTime(episodes[0].SearchStartedAt)
	if episodes[0].State != StatePreferredNotFound || !ok || !started.Equal(manager.now()) {
		t.Fatalf("reset episode did not start a new window: %+v", episodes[0])
	}
}

func TestManagerCheckNowPreservesManualAttentionReview(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackManual, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	exhaustSearchWindow(t, manager, watchID)
	before, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(before) != 1 || !before[0].AttentionCandidatesJSON.Valid {
		t.Fatalf("manual episode before check now = %#v, err = %v", before, err)
	}
	if _, err := manager.CheckNow(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(after) != 1 {
		t.Fatalf("manual episode after check now = %#v, err = %v", after, err)
	}
	if after[0].State != StateNeedsAttention || after[0].SearchAttempts != before[0].SearchAttempts || after[0].AttentionCandidatesJSON != before[0].AttentionCandidatesJSON {
		t.Fatalf("Check now disturbed manual review: before=%+v after=%+v", before[0], after[0])
	}
}

func TestPreviewOmitsDifferentSeriesTitlesButReportsTotal(t *testing.T) {
	manager, _, _, _ := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	manager.Webshare = &fakeWebshare{results: []resolver.WebshareSearchResult{
		{Ident: "right", Filename: "Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv", Size: 700 << 20, Available: true},
		{Ident: "wrong", Filename: "Other.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv", Size: 700 << 20, Available: true},
	}}
	preview, err := manager.Preview(context.Background(), CreateRequest{
		ReferenceWebshareIdent: "reference", ReferenceFilename: "Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv",
		TVMazeID: 42, SearchTitle: "Some Show", InitialMode: StartModeContinue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.TotalCandidates != 2 {
		t.Fatalf("total candidates = %d, want 2", preview.TotalCandidates)
	}
	if len(preview.Candidates) != 1 || preview.Candidates[0].Ident != "right" || !preview.Candidates[0].Exact {
		t.Fatalf("preview candidates = %+v, want only right", preview.Candidates)
	}
	// The reference episode is searched; the next tracked one is only shown.
	if preview.Episode == nil || preview.Episode.Season != 1 || preview.Episode.Episode != 1 {
		t.Fatalf("preview episode = %+v; want reference S01E01", preview.Episode)
	}
	if preview.NextEpisode == nil || preview.NextEpisode.Episode != 2 || preview.NextEpisode.EpisodeName != "Second" {
		t.Fatalf("next episode = %+v; want S01E02", preview.NextEpisode)
	}
}

func TestPreviewSearchesReferenceWithoutTVMaze(t *testing.T) {
	manager, _, _, _ := newManagerFixture(t, FallbackStrict, "Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	preview, err := manager.Preview(context.Background(), CreateRequest{
		ReferenceWebshareIdent: "reference", ReferenceFilename: "Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Episode == nil || preview.Episode.Episode != 1 || preview.NextEpisode != nil || len(preview.Candidates) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	if calls := manager.TVMaze.(*fakeTVMaze).calls; calls != 0 {
		t.Fatalf("preview without tvmaze_id called TVmaze %d times", calls)
	}
}

func TestManagerRetriesReservedSelectionAfterQueueFailure(t *testing.T) {
	manager, store, queue, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 1)
	if err := manager.processWatch(context.Background(), watchID); err == nil {
		t.Fatal("expected first queue attempt to fail")
	}
	episodes, err := store.ListEpisodes(context.Background(), watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes = %#v, err = %v", episodes, err)
	}
	if !episodes[0].ChosenWebshareIdent.Valid || episodes[0].JobID.Valid {
		t.Fatalf("selection was not durably reserved: %+v", episodes[0])
	}
	failedWatch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	if !failedWatch.LastError.Valid || !failedWatch.NextCheckAt.Valid {
		t.Fatalf("queue failure did not schedule a retry: %+v", failedWatch)
	}
	if err := manager.processWatch(context.Background(), watchID); err != nil {
		t.Fatal(err)
	}
	episode, err := store.GetEpisode(context.Background(), episodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if queue.calls != 2 || !episode.JobID.Valid || episode.State != StateQueued {
		t.Fatalf("queue retry did not recover: calls=%d episode=%+v", queue.calls, episode)
	}
}

func TestValidateOutDirUsesPathBoundary(t *testing.T) {
	manager := &Manager{AllowedRoots: []string{"/data/tv"}}
	for _, path := range []string{"/data/tv", "/data/tv/show"} {
		if err := manager.validateOutDir(path); err != nil {
			t.Fatalf("valid path %q rejected: %v", path, err)
		}
	}
	for _, path := range []string{"/data/tv-other", "/data/tv/../private", "relative"} {
		if err := manager.validateOutDir(path); err == nil {
			t.Fatalf("invalid path %q accepted", path)
		}
	}
}

func TestQueueSelectionUsesSelectedSeriesDirectory(t *testing.T) {
	store, conn := newSeriesStore(t)
	ctx := context.Background()
	watch := &Watch{
		Enabled: true, TVMazeID: 1, DisplayName: "Futurama", SearchTitle: "Futurama",
		ReferenceWebshareIdent: "reference", ReferenceFilename: "Futurama.S14E01.mkv",
		OutDir: "/tvshows/futurama", SeriesFolder: "", OrganizeBySeason: true,
	}
	watchID, err := store.CreateWatch(ctx, watch)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := store.UpsertEpisode(ctx, EpisodeInput{WatchID: watchID, TVMazeEpisodeID: 1408, Season: 14, Episode: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetEpisodeSelection(ctx, ep.ID, "candidate", "futurama.s14e08.1080p.x265.megusta.mkv", "{}"); err != nil {
		t.Fatal(err)
	}
	ep, err = store.GetEpisode(ctx, ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued := &fakeQueue{db: conn}
	manager := &Manager{Store: store, Queue: queued, AllowedRoots: []string{"/tvshows"}}
	if err := manager.queueSelection(ctx, watch, ep, queueReasonExact); err != nil {
		t.Fatal(err)
	}
	if len(queued.outDirs) != 1 || queued.outDirs[0] != "/tvshows/futurama/s14" {
		t.Fatalf("queue output directories = %v; want [/tvshows/futurama/s14]", queued.outDirs)
	}
}

func TestEpisodeOutDirCollapsesSeasonFolder(t *testing.T) {
	manager := &Manager{AllowedRoots: []string{"/data"}}
	tests := []struct {
		outDir string
		season int
		want   string
	}{
		{outDir: "/data/tvshows/Star Trek/s04", season: 4, want: "/data/tvshows/Star Trek/s04"},
		{outDir: "/data/tvshows/Show/Season 2", season: 3, want: "/data/tvshows/Show/s03"},
		{outDir: "/data/tvshows/Show", season: 1, want: "/data/tvshows/Show/s01"},
	}
	for _, test := range tests {
		got, err := manager.episodeOutDir(&Watch{OutDir: test.outDir, OrganizeBySeason: true}, test.season)
		if err != nil || got != test.want {
			t.Fatalf("episodeOutDir(%q, %d) = %q, %v; want %q", test.outDir, test.season, got, err, test.want)
		}
	}
}

func TestSeriesFolderRejectsTraversal(t *testing.T) {
	for _, folder := range []string{"../private", "shows/futurama", `shows\\futurama`, "/private", ".", ".."} {
		t.Run(folder, func(t *testing.T) {
			if _, err := normalizeSeriesFolder(folder); err == nil {
				t.Fatalf("series_folder %q was accepted", folder)
			}
		})
	}
	if _, err := normalizeSeriesFolder(""); err == nil {
		t.Fatal("empty legacy series_folder was accepted")
	}
}

func TestManagerManualAttentionSelectionUsesPersistedCandidate(t *testing.T) {
	manager, store, queue, watchID := newManagerFixture(t, FallbackManual, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	exhaustSearchWindow(t, manager, watchID)
	attention, err := manager.ListAttention(ctx, watchID)
	if err != nil || len(attention) != 1 || len(attention[0].Candidates) != 1 {
		t.Fatalf("attention = %#v, err = %v", attention, err)
	}
	ep := attention[0]
	if _, err := manager.SelectAttentionCandidate(ctx, watchID, ep.ID, "not-from-snapshot"); err == nil {
		t.Fatal("arbitrary candidate was accepted")
	}
	if _, err := manager.SelectAttentionCandidate(ctx, watchID+1, ep.ID, ep.Candidates[0].Ident); err == nil {
		t.Fatal("cross-watch candidate selection was accepted")
	}
	selected, err := manager.SelectAttentionCandidate(ctx, watchID, ep.ID, ep.Candidates[0].Ident)
	if err != nil {
		t.Fatal(err)
	}
	if selected.JobID == 0 || queue.calls != 1 {
		t.Fatalf("selected = %+v calls=%d", selected, queue.calls)
	}
	// Repeating the same request is safe after a client timeout.
	if _, err := manager.SelectAttentionCandidate(ctx, watchID, ep.ID, ep.Candidates[0].Ident); err != nil {
		t.Fatalf("repeated selection: %v", err)
	}
	if queue.calls != 1 {
		t.Fatalf("repeated selection queued another job: calls=%d", queue.calls)
	}
	stored, err := store.GetEpisode(ctx, ep.ID)
	if err != nil || !stored.ChosenFilename.Valid || stored.ChosenFilename.String != ep.Candidates[0].Filename {
		t.Fatalf("stored selection = %+v, err = %v", stored, err)
	}
}

func TestManagerRejectsMalformedAttentionSnapshot(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackManual, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	exhaustSearchWindow(t, manager, watchID)
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes=%v err=%v", episodes, err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE series_episodes SET attention_candidates_json = '{' WHERE id = ?`, episodes[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ListAttention(ctx, watchID); err == nil {
		t.Fatal("malformed snapshot was accepted")
	}
	if _, err := manager.SelectAttentionCandidate(ctx, watchID, episodes[0].ID, "candidate"); err == nil {
		t.Fatal("malformed snapshot could be selected")
	}
}

func TestManagerCreateDoesNotSynchronouslyCallTVMaze(t *testing.T) {
	store, _ := newSeriesStore(t)
	tvmaze := &fakeTVMaze{}
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	manager := &Manager{Store: store, TVMaze: tvmaze, AllowedRoots: []string{"/data"}, Now: func() time.Time { return now }}
	watch, err := manager.Create(context.Background(), CreateRequest{
		ReferenceWebshareIdent: "abcde", ReferenceFilename: "Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv",
		OutDir: "/data/tv", TVMazeID: 42, DisplayName: "Some Show",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tvmaze.calls != 0 {
		t.Fatalf("Create called TVmaze %d times", tvmaze.calls)
	}
	if watch.NextCheckAt == "" {
		t.Fatal("created watch is not due for the scheduler")
	}
}

func TestManagerCreateUsesDefaultsOnlyWhenDelaysAreOmitted(t *testing.T) {
	store, _ := newSeriesStore(t)
	manager := &Manager{Store: store, AllowedRoots: []string{"/data"}}
	base := CreateRequest{
		ReferenceWebshareIdent: "abcde", ReferenceFilename: "Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv",
		OutDir: "/data/tv", TVMazeID: 42, DisplayName: "Some Show",
	}
	defaults, err := manager.Create(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.PreferredWaitSeconds != int64(DefaultPreferredWait/time.Second) {
		t.Fatalf("default preferred wait = %d", defaults.PreferredWaitSeconds)
	}
	zero := int64(0)
	base.PreferredWaitSeconds = &zero
	explicitZero, err := manager.Create(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if explicitZero.PreferredWaitSeconds != 0 {
		t.Fatalf("explicit zero preferred wait = %d", explicitZero.PreferredWaitSeconds)
	}
	negative := int64(-1)
	base.PreferredWaitSeconds = &negative
	if _, err := manager.Create(context.Background(), base); err == nil {
		t.Fatal("negative preferred wait was accepted")
	} else {
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("negative delay error = %T %v, want ValidationError", err, err)
		}
	}
}

func TestManagerSchedulesKnownEpisodeAtAirEnd(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	air := now.Add(12 * time.Hour)
	runtime := 30
	tests := []struct {
		name    string
		runtime *int
		want    time.Time
		event   string
	}{
		{name: "runtime", runtime: &runtime, want: air.Add(30 * time.Minute), event: `no episode due; next S01E02 "Second" ends 2026-09-19 00:30 UTC`},
		{name: "fallback", runtime: nil, want: air.Add(defaultEpisodeRuntime), event: `no episode due; next S01E02 "Second" ends 2026-09-19 01:00 UTC`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
			number := 2
			manager.TVMaze = &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air, Runtime: test.runtime}}}
			if err := manager.processWatch(context.Background(), watchID); err != nil {
				t.Fatal(err)
			}
			if got := watchNextCheck(t, store, watchID); !got.Equal(test.want) {
				t.Fatalf("next check = %v; want air end %v", got, test.want)
			}
			if got := latestEventMessage(t, store, watchID); got != test.event {
				t.Fatalf("event = %q; want %q", got, test.event)
			}
		})
	}
}

func TestManagerRefreshesMetadataDailyForDistantEpisode(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	air := now.Add(8 * 24 * time.Hour)
	number := 2
	manager.TVMaze = &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}
	if err := manager.processWatch(context.Background(), watchID); err != nil {
		t.Fatal(err)
	}
	watch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseNullTime(watch.NextCheckAt)
	if want := now.Add(24 * time.Hour); !ok || !got.Equal(want) {
		t.Fatalf("next metadata refresh = %v; want %v", got, want)
	}
}

func TestManagerUpdateReschedulesWithoutClearingLastCheck(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Hour)
	next := now.Add(7 * 24 * time.Hour)
	if err := store.UpdateWatchCheck(context.Background(), watchID, &last, &next, ""); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if _, err := manager.Update(context.Background(), watchID, UpdateRequest{PreferredWaitSeconds: &zero}); err != nil {
		t.Fatal(err)
	}
	watch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	gotNext, nextOK := parseNullTime(watch.NextCheckAt)
	gotLast, lastOK := parseNullTime(watch.LastCheckedAt)
	if !nextOK || !gotNext.Equal(now) {
		t.Fatalf("next check = %v; want %v", gotNext, now)
	}
	if !lastOK || !gotLast.Equal(last) {
		t.Fatalf("last check = %v; want preserved %v", gotLast, last)
	}
}

func TestManagerUpdateDoesNotAppendDeprecatedSeriesFolder(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	outDir := "/data/tv/some-show"
	legacyFolder := "some-show"
	if _, err := manager.Update(context.Background(), watchID, UpdateRequest{OutDir: &outDir, SeriesFolder: &legacyFolder}); err != nil {
		t.Fatal(err)
	}
	watch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	if watch.OutDir != outDir || watch.SeriesFolder != "" {
		t.Fatalf("updated path = %q + %q; want exact %q", watch.OutDir, watch.SeriesFolder, outDir)
	}
}

func TestManagerCheckNowRejectsPausedWatch(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	if err := store.SetWatchEnabled(context.Background(), watchID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CheckNow(context.Background(), watchID); !errors.Is(err, ErrWatchPaused) {
		t.Fatalf("CheckNow error = %v, want ErrWatchPaused", err)
	}
	if calls := manager.TVMaze.(*fakeTVMaze).calls; calls != 0 {
		t.Fatalf("paused watch called TVmaze %d times", calls)
	}
}

func TestManagerCheckNowOnlySerializesSameWatch(t *testing.T) {
	store, _ := newSeriesStore(t)
	watchID, err := store.CreateWatch(context.Background(), &Watch{
		Enabled: true, TVMazeID: 42, DisplayName: "Some Show", SearchTitle: "Some Show",
		ReferenceWebshareIdent: "reference", ReferenceFilename: "Some.Show.S01E01.mkv", OutDir: "/data",
	})
	if err != nil {
		t.Fatal(err)
	}
	tvmaze := &blockingTVMaze{started: make(chan struct{}), release: make(chan struct{})}
	manager := &Manager{Store: store, TVMaze: tvmaze, AllowedRoots: []string{"/data"}}
	first := make(chan error, 1)
	go func() {
		_, err := manager.CheckNow(context.Background(), watchID)
		first <- err
	}()
	<-tvmaze.started
	if _, err := manager.CheckNow(context.Background(), watchID); !errors.Is(err, ErrWatchCheckInProgress) {
		t.Fatalf("concurrent CheckNow error = %v, want ErrWatchCheckInProgress", err)
	}
	close(tvmaze.release)
	if err := <-first; err != nil {
		t.Fatalf("first CheckNow: %v", err)
	}
}

func TestManagerCheckDueRunsDifferentWatchesInParallel(t *testing.T) {
	store, _ := newSeriesStore(t)
	for _, name := range []string{"One", "Two"} {
		if _, err := store.CreateWatch(context.Background(), &Watch{
			Enabled: true, TVMazeID: 42, DisplayName: name, SearchTitle: name,
			ReferenceWebshareIdent: "reference", ReferenceFilename: name + ".S01E01.mkv", OutDir: "/data",
		}); err != nil {
			t.Fatal(err)
		}
	}
	tvmaze := &parallelTVMaze{started: make(chan struct{}, 2), release: make(chan struct{})}
	manager := &Manager{Store: store, TVMaze: tvmaze, AllowedRoots: []string{"/data"}}
	done := make(chan error, 1)
	go func() { done <- manager.CheckDue(context.Background()) }()
	for i := 0; i < 2; i++ {
		select {
		case <-tvmaze.started:
		case <-time.After(time.Second):
			close(tvmaze.release)
			t.Fatal("due watches did not start in parallel")
		}
	}
	close(tvmaze.release)
	if err := <-done; err != nil {
		t.Fatalf("CheckDue: %v", err)
	}
}

func TestManagerQueueRetryAttachesExistingSourceKeyJob(t *testing.T) {
	store, conn := newSeriesStore(t)
	ctx := context.Background()
	watch := &Watch{Enabled: true, TVMazeID: 42, DisplayName: "Some Show", SearchTitle: "Some Show", ReferenceWebshareIdent: "reference", ReferenceFilename: "Some.Show.S01E01.mkv", OutDir: "/data"}
	watchID, err := store.CreateWatch(ctx, watch)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := store.UpsertEpisode(ctx, EpisodeInput{WatchID: watchID, TVMazeEpisodeID: 1002, Season: 1, Episode: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetEpisodeSelection(ctx, ep.ID, "candidate", "Some.Show.S01E02.mkv", "{}"); err != nil {
		t.Fatal(err)
	}
	jobService := queue.NewService(queue.NewStore(conn), nil, []string{"/data"})
	// Simulate a crash after the idempotent job insert but before AttachJob.
	jobID, err := jobService.CreateJobWithSourceKey(ctx, "https://webshare.cz/#/file/candidate", "/data", "", "webshare", "", 1, "series:1:episode:1002")
	if err != nil {
		t.Fatal(err)
	}
	ep, err = store.GetEpisode(ctx, ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: store, Queue: jobService, AllowedRoots: []string{"/data"}}
	if err := manager.queueSelection(ctx, watch, ep, queueReasonExact); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetEpisode(ctx, ep.ID)
	if err != nil || !updated.JobID.Valid || updated.JobID.Int64 != jobID {
		t.Fatalf("episode=%+v err=%v want job=%d", updated, err, jobID)
	}
	jobs, err := jobService.ListJobs(ctx, "", true)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
}

func countEvents(t *testing.T, store *Store, watchID int64, message string) int {
	t.Helper()
	events, err := store.ListEvents(context.Background(), watchID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range events {
		if e.Message == message {
			count++
		}
	}
	return count
}

func countEventsWithPrefix(t *testing.T, store *Store, watchID int64, prefix string) int {
	t.Helper()
	events, err := store.ListEvents(context.Background(), watchID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range events {
		if strings.HasPrefix(e.Message, prefix) {
			count++
		}
	}
	return count
}

func episodeState(t *testing.T, store *Store, watchID int64) string {
	t.Helper()
	episodes, err := store.ListEpisodes(context.Background(), watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes = %+v, err = %v", episodes, err)
	}
	return episodes[0].State
}

// newQueuedEpisodeFixture returns a watch whose only episode has queued job #1
// and whose next check is a day away.
func newQueuedEpisodeFixture(t *testing.T) (*Manager, *Store, int64) {
	t.Helper()
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	if err := manager.processWatch(context.Background(), watchID); err != nil {
		t.Fatal(err)
	}
	if state := episodeState(t, store, watchID); state != StateQueued {
		t.Fatalf("episode state = %q; want queued", state)
	}
	return manager, store, watchID
}

func TestCheckDueSyncsJobStateWithoutWatchCheck(t *testing.T) {
	manager, store, watchID := newQueuedEpisodeFixture(t)
	ctx := context.Background()
	tvmaze := manager.TVMaze.(*fakeTVMaze)
	checks := tvmaze.calls
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }
	for i := 0; i < 2; i++ {
		if err := manager.CheckDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if state := episodeState(t, store, watchID); state != StateCompleted {
		t.Fatalf("episode state = %q; want completed", state)
	}
	if tvmaze.calls != checks {
		t.Fatalf("watch check ran: TVmaze calls %d -> %d", checks, tvmaze.calls)
	}
	if got := countEvents(t, store, watchID, "S01E02 job #1 completed"); got != 1 {
		t.Fatalf("completion events = %d; want 1", got)
	}
}

func TestSyncJobStateAppendsFailureError(t *testing.T) {
	manager, store, watchID := newQueuedEpisodeFixture(t)
	ctx := context.Background()
	manager.JobState = func(context.Context, int64) (JobStatus, error) {
		return JobStatus{Status: StateFailed, Error: "boom"}, nil
	}
	if err := manager.CheckDue(ctx); err != nil {
		t.Fatal(err)
	}
	if state := episodeState(t, store, watchID); state != StateFailed {
		t.Fatalf("episode state = %q; want failed", state)
	}
	if got := countEvents(t, store, watchID, "S01E02 job #1 failed: boom"); got != 1 {
		t.Fatalf("failure events = %d; want 1", got)
	}
}

func TestCheckDueSkipsJobSyncDuringWatchCheck(t *testing.T) {
	manager, store, watchID := newQueuedEpisodeFixture(t)
	ctx := context.Background()
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }
	if !manager.startWatchRun(watchID) {
		t.Fatal("watch already running")
	}
	if err := manager.CheckDue(ctx); err != nil {
		t.Fatal(err)
	}
	if state := episodeState(t, store, watchID); state != StateQueued {
		t.Fatalf("episode state during running check = %q; want queued", state)
	}
	manager.finishWatchRun(watchID)
	if err := manager.CheckDue(ctx); err != nil {
		t.Fatal(err)
	}
	if state := episodeState(t, store, watchID); state != StateCompleted {
		t.Fatalf("episode state after check = %q; want completed", state)
	}
}

func TestManagerFetchesShowOnlyWhenIdle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	// The first check of a watch always fetches the show for its timezone.
	upcoming, upcomingStore, _, upcomingID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	if err := upcomingStore.SetWatchShow(ctx, upcomingID, "", ""); err != nil {
		t.Fatal(err)
	}
	air := now.Add(12 * time.Hour)
	number := 2
	upcomingTVMaze := &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}
	upcoming.TVMaze = upcomingTVMaze
	if err := upcoming.processWatch(ctx, upcomingID); err != nil {
		t.Fatal(err)
	}
	if upcomingTVMaze.showCalls != 0 {
		t.Fatalf("show fetched with an upcoming episode: calls=%d", upcomingTVMaze.showCalls)
	}

	searching, searchingStore, _, searchingID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	if err := searchingStore.SetWatchShow(ctx, searchingID, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := searching.processWatch(ctx, searchingID); err != nil {
		t.Fatal(err)
	}
	if calls := searching.TVMaze.(*fakeTVMaze).showCalls; calls != 0 {
		t.Fatalf("show fetched while searching: calls=%d", calls)
	}

	manager, store, watchID := newQueuedEpisodeFixture(t)
	tvmaze := manager.TVMaze.(*fakeTVMaze)
	tvmaze.showStatus = "Running"
	baseline := tvmaze.showCalls
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateDownloading}, nil }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if tvmaze.showCalls != baseline {
		t.Fatalf("show fetched with an unfinished job: calls %d -> %d", baseline, tvmaze.showCalls)
	}
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if tvmaze.showCalls != baseline+1 {
		t.Fatalf("show calls when idle = %d; want %d", tvmaze.showCalls, baseline+1)
	}
	if got := watchNextCheck(t, store, watchID); !got.Equal(now.Add(metadataRefreshInterval)) {
		t.Fatalf("next check = %v; want +24h", got)
	}
	if got, want := latestEventMessage(t, store, watchID), "no episode due; waiting for next season"; got != want {
		t.Fatalf("event = %q; want %q", got, want)
	}
	view, err := manager.Get(ctx, watchID)
	if err != nil || view.ShowStatus != "Running" {
		t.Fatalf("view = %+v, err = %v", view, err)
	}
}

func TestManagerSchedulesEndedShowWeekly(t *testing.T) {
	manager, store, watchID := newQueuedEpisodeFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	manager.TVMaze.(*fakeTVMaze).showStatus = tvmazeShowEnded
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := watchNextCheck(t, store, watchID); !got.Equal(now.Add(endedShowCheckInterval)) {
		t.Fatalf("next check = %v; want +7d", got)
	}
	if got, want := latestEventMessage(t, store, watchID), "no episode due; series ended, next check 2026-09-25 12:00 UTC"; got != want {
		t.Fatalf("event = %q; want %q", got, want)
	}
	view, err := manager.Get(ctx, watchID)
	if err != nil || view.ShowStatus != tvmazeShowEnded {
		t.Fatalf("view = %+v, err = %v", view, err)
	}
}

func TestEpisodeViewUpdatedAtKeepsCompletionTime(t *testing.T) {
	manager, store, watchID := newQueuedEpisodeFixture(t)
	ctx := context.Background()
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }
	if err := manager.CheckDue(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := manager.Get(ctx, watchID)
	if err != nil || view.LastEpisode == nil || view.LastEpisode.UpdatedAt == "" {
		t.Fatalf("view = %+v, err = %v", view, err)
	}
	completedAt := view.LastEpisode.UpdatedAt
	encoded, err := json.Marshal(view.LastEpisode)
	if err != nil || !strings.Contains(string(encoded), `"updated_at":"`+completedAt+`"`) {
		t.Fatalf("episode JSON = %s, err = %v", encoded, err)
	}
	// A later check re-syncs unchanged TVmaze metadata.
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || episodes[0].UpdatedAt != completedAt {
		t.Fatalf("updated_at after metadata sync = %+v; want %s, err = %v", episodes, completedAt, err)
	}
}

func TestManagerSearchableAtAirDate(t *testing.T) {
	// Within the daily metadata refresh of the fixture's now (09-18 12:00).
	placeholder := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	runtime := 30
	tests := []struct {
		name     string
		timezone string
		airtime  string
		want     time.Time
	}{
		{name: "date-only EDT", timezone: "America/New_York", want: time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC)},
		{name: "date-only no timezone", want: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)},
		{name: "airtime known", timezone: "America/New_York", airtime: "12:00", want: placeholder.Add(30 * time.Minute)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
			number := 2
			manager.TVMaze = &fakeTVMaze{showTimezone: test.timezone, episodes: []TVMazeEpisode{{
				ID: 1002, Name: "Second", Season: 1, Number: &number, Airdate: "2026-09-19",
				Airtime: test.airtime, Airstamp: &placeholder, Runtime: &runtime,
			}}}
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			if got := watchNextCheck(t, store, watchID); !got.Equal(test.want) {
				t.Fatalf("next check = %v; want %v", got, test.want)
			}
			view, err := manager.Get(ctx, watchID)
			if err != nil {
				t.Fatal(err)
			}
			if want := test.want.UTC().Format(time.RFC3339); view.NextSearchAt != want {
				t.Fatalf("next search at = %q; want %q", view.NextSearchAt, want)
			}
			if state := episodeState(t, store, watchID); state != StateWaitingRelease {
				t.Fatalf("state before searchable = %q; want waiting_release", state)
			}
			manager.Now = func() time.Time { return test.want }
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			if state := episodeState(t, store, watchID); state != StateQueued {
				t.Fatalf("state at searchable time = %q; want queued", state)
			}
		})
	}
}

func TestViewTreatsSearchableDateOnlyEpisodeAsLast(t *testing.T) {
	ctx := context.Background()
	manager, _, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	placeholder := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	number := 2
	manager.TVMaze = &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airdate: "2026-09-19", Airstamp: &placeholder}}}
	manager.Now = func() time.Time { return time.Date(2026, 9, 19, 0, 30, 0, 0, time.UTC) }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	view, err := manager.Get(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	if view.NextEpisode != nil {
		t.Fatalf("next episode = %+v; want nil before the placeholder airstamp", view.NextEpisode)
	}
	if view.LastEpisode == nil || view.LastEpisode.Episode != 2 {
		t.Fatalf("last episode = %+v; want S01E02", view.LastEpisode)
	}
}

func TestSyncJobStateSkipsEpisodeWhenJobRemoved(t *testing.T) {
	tests := []struct {
		name     string
		jobState func(context.Context, int64) (JobStatus, error)
	}{
		{name: "deleted", jobState: func(context.Context, int64) (JobStatus, error) {
			return JobStatus{Status: queue.StatusDeleted}, nil
		}},
		{name: "purged", jobState: func(context.Context, int64) (JobStatus, error) {
			return JobStatus{}, sql.ErrNoRows
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, store, watchID := newQueuedEpisodeFixture(t)
			ctx := context.Background()
			manager.JobState = test.jobState
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			if state := episodeState(t, store, watchID); state != StateSkipped {
				t.Fatalf("episode state = %q; want skipped", state)
			}
			want := "S01E02 job #1 removed from queue; episode skipped"
			if got := countEvents(t, store, watchID, want); got != 1 {
				t.Fatalf("skip events = %d; want 1", got)
			}
			// The episode is no longer in flight once skipped: a later sync must
			// not re-skip it or duplicate the event.
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			if got := countEvents(t, store, watchID, want); got != 1 {
				t.Fatalf("skip events = %d; want 1", got)
			}
		})
	}
}

func TestSyncJobStateIgnoresRemovedJobOnFinalEpisode(t *testing.T) {
	tests := []struct {
		name     string
		jobState func(context.Context, int64) (JobStatus, error)
	}{
		{name: "deleted", jobState: func(context.Context, int64) (JobStatus, error) {
			return JobStatus{Status: queue.StatusDeleted}, nil
		}},
		{name: "purged", jobState: func(context.Context, int64) (JobStatus, error) {
			return JobStatus{}, sql.ErrNoRows
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, store, watchID := newQueuedEpisodeFixture(t)
			ctx := context.Background()
			manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			if state := episodeState(t, store, watchID); state != StateCompleted {
				t.Fatalf("episode state = %q; want completed", state)
			}
			skipMessage := "S01E02 job #1 removed from queue; episode skipped"
			before := countEvents(t, store, watchID, skipMessage)

			// A completed episode's job later shows up removed (cleared or
			// purged). The download already happened; the episode must not be
			// disturbed.
			manager.JobState = test.jobState
			if err := manager.processWatch(ctx, watchID); err != nil {
				t.Fatal(err)
			}
			if state := episodeState(t, store, watchID); state != StateCompleted {
				t.Fatalf("completed episode state changed to %q after job removal", state)
			}
			if got := countEvents(t, store, watchID, skipMessage); got != before {
				t.Fatalf("skip events = %d; want unchanged from %d", got, before)
			}
		})
	}
}

func TestManagerFetchesTimezoneOnce(t *testing.T) {
	ctx := context.Background()
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	air := now.Add(48 * time.Hour)
	number := 2
	tvmaze := &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airdate: "2026-09-20", Airstamp: &air}}}
	manager.TVMaze = tvmaze
	for i := 0; i < 2; i++ {
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
	if tvmaze.showCalls != 1 {
		t.Fatalf("show calls = %d; want 1", tvmaze.showCalls)
	}
	watch, err := store.GetWatch(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	if !watch.AirTimezone.Valid || watch.AirTimezone.String != "" {
		t.Fatalf("air timezone = %+v; want fetched and empty", watch.AirTimezone)
	}
}

func TestManagerLogsAddedEpisodesAfterResume(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	number := 2
	air := now.Add(12 * time.Hour)
	tvmaze := &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}
	manager.TVMaze = tvmaze
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		if _, err := manager.SetEnabled(ctx, watchID, enabled); err != nil {
			t.Fatal(err)
		}
	}
	w, err := store.GetWatch(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	if !w.LastCheckedAt.Valid {
		t.Fatal("resume cleared last_checked_at")
	}

	three := 3
	airThree := now.Add(36 * time.Hour)
	tvmaze.episodes = append(tvmaze.episodes, TVMazeEpisode{ID: 1003, Name: "Third", Season: 1, Number: &three, Airstamp: &airThree})
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "TVmaze added 1 episode: S01E03"); got != 1 {
		t.Fatalf("added events after resume = %d; want 1", got)
	}
}

func TestManagerLogsFirstSeasonAnnouncedAfterOffSeason(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	tvmaze := &fakeTVMaze{}
	manager.TVMaze = tvmaze
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}

	number := 2
	air := now.Add(12 * time.Hour)
	tvmaze.episodes = []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "TVmaze added 1 episode: S01E02"); got != 1 {
		t.Fatalf("added events after off-season = %d; want 1", got)
	}
}

func TestManagerLogsTVMazeEpisodeChanges(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	manager, store, queue, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	number := 2
	air := now.Add(12 * time.Hour)
	tvmaze := &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}
	manager.TVMaze = tvmaze

	// First check: nothing was stored before, so this is a plain count, not a
	// per-episode "added" list.
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "tracking 1 episode from TVmaze"); got != 1 {
		t.Fatalf("first-check summary events = %d; want 1", got)
	}

	// New episodes show up on a later check as a per-episode list.
	three, four := 3, 4
	airThree, airFour := now.Add(36*time.Hour), now.Add(60*time.Hour)
	tvmaze.episodes = append(tvmaze.episodes,
		TVMazeEpisode{ID: 1003, Name: "Third", Season: 1, Number: &three, Airstamp: &airThree},
		TVMazeEpisode{ID: 1004, Name: "Fourth", Season: 1, Number: &four, Airstamp: &airFour})
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "TVmaze added 2 episodes: S01E03, S01E04"); got != 1 {
		t.Fatalf("new episode events = %d; want 1", got)
	}

	// A schedule correction on the still-scheduled first episode is logged.
	newAir := air.Add(2 * time.Hour)
	tvmaze.episodes[0].Airstamp = &newAir
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("S01E02 air time changed: %s → %s", formatEventTime(air), formatEventTime(newAir))
	if got := countEvents(t, store, watchID, want); got != 1 {
		t.Fatalf("air time change events = %d; want 1 (message %q)", got, want)
	}

	// An unchanged episode logs nothing more on a later check.
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEventsWithPrefix(t, store, watchID, "S01E02 air time changed"); got != 1 {
		t.Fatalf("air time change events after an unchanged check = %d; want 1", got)
	}

	// A final episode no longer acts on its schedule, so a further TVmaze
	// correction is not worth an event.
	ep, err := store.GetEpisodeByTVMazeID(ctx, watchID, 1002)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := queue.CreateJob(ctx, "https://webshare.cz/#/file/candidate", "/data/tv", "", "webshare", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AttachJob(ctx, ep.ID, jobID, StateCompleted); err != nil {
		t.Fatal(err)
	}
	finalAir := newAir.Add(2 * time.Hour)
	tvmaze.episodes[0].Airstamp = &finalAir
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEventsWithPrefix(t, store, watchID, "S01E02 air time changed"); got != 1 {
		t.Fatalf("final episode logged an air time change: %d events", got)
	}
}

func TestManagerLogsShowStatusAndTimezoneChanges(t *testing.T) {
	ctx := context.Background()
	manager, store, watchID := newQueuedEpisodeFixture(t)
	tvmaze := manager.TVMaze.(*fakeTVMaze)
	manager.JobState = func(context.Context, int64) (JobStatus, error) { return JobStatus{Status: StateCompleted}, nil }

	// First fetch of a non-empty status/timezone establishes the baseline;
	// nothing was known before, so no change event fires yet.
	tvmaze.showStatus = "Running"
	tvmaze.showTimezone = "America/New_York"
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "show status  → Running"); got != 0 {
		t.Fatalf("baseline show status logged a change: %d events", got)
	}
	if got := countEvents(t, store, watchID, "air timezone  → America/New_York"); got != 0 {
		t.Fatalf("baseline air timezone logged a change: %d events", got)
	}

	// A later, actual change from a known prior value is logged.
	tvmaze.showStatus = "Ended"
	tvmaze.showTimezone = "Europe/Prague"
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "show status Running → Ended"); got != 1 {
		t.Fatalf("show status change events = %d; want 1", got)
	}
	if got := countEvents(t, store, watchID, "air timezone America/New_York → Europe/Prague"); got != 1 {
		t.Fatalf("air timezone change events = %d; want 1", got)
	}
}

func TestManagerSetEnabledLogsOnlyOnActualChange(t *testing.T) {
	ctx := context.Background()
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)

	// Already enabled: re-enabling is a no-op and must not log anything.
	if _, err := manager.SetEnabled(ctx, watchID, true); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "watch resumed"); got != 0 {
		t.Fatalf("no-op enable logged an event: %d", got)
	}

	if _, err := manager.SetEnabled(ctx, watchID, false); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "watch paused"); got != 1 {
		t.Fatalf("pause events = %d; want 1", got)
	}

	// Already paused: pausing again must not duplicate the event.
	if _, err := manager.SetEnabled(ctx, watchID, false); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "watch paused"); got != 1 {
		t.Fatalf("pause events after no-op = %d; want 1", got)
	}

	if _, err := manager.SetEnabled(ctx, watchID, true); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, watchID, "watch resumed"); got != 1 {
		t.Fatalf("resume events = %d; want 1", got)
	}
}

func TestManagerUpdateLogsChangedFieldsOnly(t *testing.T) {
	ctx := context.Background()
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)

	// No editable field actually changes: no event.
	sameTitle := "Some Show"
	if _, err := manager.Update(ctx, watchID, UpdateRequest{SearchTitle: &sameTitle}); err != nil {
		t.Fatal(err)
	}
	if got := countEventsWithPrefix(t, store, watchID, "settings changed"); got != 0 {
		t.Fatalf("no-op update logged an event: got %d matching \"settings changed\"", got)
	}

	newTitle := "Some Show (2026)"
	newOutDir := "/data/tv/some-show"
	if _, err := manager.Update(ctx, watchID, UpdateRequest{SearchTitle: &newTitle, OutDir: &newOutDir}); err != nil {
		t.Fatal(err)
	}
	want := "settings changed: out_dir, search_title"
	if got := countEvents(t, store, watchID, want); got != 1 {
		t.Fatalf("update events = %d; want 1 (message %q)", got, want)
	}
}
