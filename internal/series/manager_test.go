package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Witriol/dlq-download-queue/internal/queue"
	"github.com/Witriol/dlq-download-queue/internal/resolver"
)

type fakeTVMaze struct {
	episodes []TVMazeEpisode
	calls    int
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

func (f *blockingTVMaze) SearchShows(context.Context, string) ([]TVMazeSearchResult, error) {
	return nil, nil
}

func (f *blockingTVMaze) Episodes(context.Context, int64) ([]TVMazeEpisode, error) {
	close(f.started)
	<-f.release
	return nil, nil
}

func (f *fakeTVMaze) SearchShows(context.Context, string) ([]TVMazeSearchResult, error) {
	return nil, nil
}

func (f *fakeTVMaze) Episodes(context.Context, int64) ([]TVMazeEpisode, error) {
	f.calls++
	return f.episodes, nil
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
		FallbackPolicy: policy, ReleaseDelaySeconds: 1, PreferredWaitSeconds: 1,
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

func TestManagerFallbackPolicies(t *testing.T) {
	const alternative = "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv"
	tests := []struct {
		policy    string
		wantCalls int
		wantState string
	}{
		{policy: FallbackStrict, wantCalls: 0, wantState: StatePreferredNotFound},
		{policy: FallbackBalanced, wantCalls: 1, wantState: StateQueued},
		{policy: FallbackManual, wantCalls: 0, wantState: StatePreferredNotFound},
	}
	for _, test := range tests {
		t.Run(test.policy, func(t *testing.T) {
			manager, store, queue, watchID := newManagerFixture(t, test.policy, alternative, 0)
			if err := manager.processWatch(context.Background(), watchID); err != nil {
				t.Fatal(err)
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

func TestManagerStopsAfterFourReleaseSearchesAndWarns(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	for attempt := 1; attempt <= MaxReleaseSearches; attempt++ {
		now := start.Add(time.Duration(attempt-1) * ReleaseRetryDelay)
		manager.Now = func() time.Time { return now }
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
		episodes, err := store.ListEpisodes(ctx, watchID)
		if err != nil || len(episodes) != 1 {
			t.Fatalf("episodes = %#v, err = %v", episodes, err)
		}
		if episodes[0].SearchAttempts != attempt {
			t.Fatalf("attempt %d persisted search_attempts = %d", attempt, episodes[0].SearchAttempts)
		}
		if attempt < MaxReleaseSearches {
			watch, err := store.GetWatch(ctx, watchID)
			if err != nil {
				t.Fatal(err)
			}
			next, ok := parseNullTime(watch.NextCheckAt)
			if !ok || !next.Equal(now.Add(ReleaseRetryDelay)) {
				t.Fatalf("attempt %d next check = %v; want %v", attempt, next, now.Add(ReleaseRetryDelay))
			}
		} else if episodes[0].State != StateNeedsAttention {
			t.Fatalf("final state = %q; want %q", episodes[0].State, StateNeedsAttention)
		}
	}

	// A later scheduler pass leaves the exhausted episode alone.
	manager.Now = func() time.Time { return start.Add(24 * time.Hour) }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || episodes[0].SearchAttempts != MaxReleaseSearches {
		t.Fatalf("exhausted episode was searched again: episodes=%+v err=%v", episodes, err)
	}
	view, err := manager.Get(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	if view.AttentionCount != 1 || view.Status != StateNeedsAttention {
		t.Fatalf("warning view = %+v", view)
	}
}

func TestManagerBalancedFallbackUsesFinalBoundedSearchAtPreferredWait(t *testing.T) {
	manager, store, queue, watchID := newManagerFixture(t, FallbackBalanced, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	number := 2
	air := start.Add(-DefaultReleaseDelay)
	manager.TVMaze = &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}
	watch, err := store.GetWatch(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	watch.ReleaseDelaySeconds = int64(DefaultReleaseDelay / time.Second)
	watch.PreferredWaitSeconds = int64(DefaultPreferredWait / time.Second)
	if err := store.UpdateWatch(ctx, watch); err != nil {
		t.Fatal(err)
	}

	// The first three bounded checks retain the preferred-quality wait. The
	// final check is scheduled at its expiry rather than exhausting the cycle
	// eight hours after airtime.
	for attempt := 1; attempt <= MaxReleaseSearches-1; attempt++ {
		now := start.Add(time.Duration(attempt-1) * ReleaseRetryDelay)
		manager.Now = func() time.Time { return now }
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
	updated, err := store.GetWatch(ctx, watchID)
	if err != nil {
		t.Fatal(err)
	}
	next, ok := parseNullTime(updated.NextCheckAt)
	wantFallbackCheck := start.Add(DefaultPreferredWait)
	if !ok || !next.Equal(wantFallbackCheck) {
		t.Fatalf("balanced fallback check = %v; want %v", next, wantFallbackCheck)
	}

	manager.Now = func() time.Time { return wantFallbackCheck }
	if err := manager.processWatch(ctx, watchID); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes = %#v, err = %v", episodes, err)
	}
	if queue.calls != 1 || episodes[0].State != StateQueued {
		t.Fatalf("balanced fallback did not queue alternative: calls=%d episode=%+v", queue.calls, episodes[0])
	}
}

func TestManagerCheckNowResetsExhaustedAutomaticEpisode(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	for range MaxReleaseSearches {
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
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
}

func TestManagerUpdateResetsExhaustedAutomaticEpisode(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	for range MaxReleaseSearches {
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
	updatedTitle := "Some Show (2026)"
	if _, err := manager.Update(ctx, watchID, UpdateRequest{SearchTitle: &updatedTitle}); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.ListEpisodes(ctx, watchID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes after update = %#v, err = %v", episodes, err)
	}
	if episodes[0].State != StateScheduled || episodes[0].SearchAttempts != 0 || episodes[0].AttentionCandidatesJSON.Valid {
		t.Fatalf("settings update did not reset exhausted episode: %+v", episodes[0])
	}
}

func TestManagerCheckNowPreservesManualAttentionReview(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackManual, "Some.Show.S01E02.1080p.WEB-DL.x265-Other.mkv", 0)
	ctx := context.Background()
	for range MaxReleaseSearches {
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
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
	if after[0].State != StateNeedsAttention || after[0].SearchAttempts != MaxReleaseSearches || after[0].AttentionCandidatesJSON != before[0].AttentionCandidatesJSON {
		t.Fatalf("Check now disturbed manual review: before=%+v after=%+v", before[0], after[0])
	}
}

func TestPreviewOmitsDifferentSeriesTitlesButReportsTotal(t *testing.T) {
	manager, _, _, _ := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	manager.Webshare = &fakeWebshare{results: []resolver.WebshareSearchResult{
		{Ident: "right", Filename: "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", Size: 700 << 20, Available: true},
		{Ident: "wrong", Filename: "Other.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", Size: 700 << 20, Available: true},
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
	if len(preview.Candidates) != 1 || preview.Candidates[0].Ident != "right" {
		t.Fatalf("preview candidates = %+v, want only right", preview.Candidates)
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
	if err := manager.queueSelection(ctx, watch, ep); err != nil {
		t.Fatal(err)
	}
	if len(queued.outDirs) != 1 || queued.outDirs[0] != "/tvshows/futurama/s14" {
		t.Fatalf("queue output directories = %v; want [/tvshows/futurama/s14]", queued.outDirs)
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
	for range MaxReleaseSearches {
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
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
	for range MaxReleaseSearches {
		if err := manager.processWatch(ctx, watchID); err != nil {
			t.Fatal(err)
		}
	}
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
	if defaults.ReleaseDelaySeconds != int64(DefaultReleaseDelay/time.Second) || defaults.PreferredWaitSeconds != int64(DefaultPreferredWait/time.Second) {
		t.Fatalf("default delays = %d, %d", defaults.ReleaseDelaySeconds, defaults.PreferredWaitSeconds)
	}
	zero := int64(0)
	base.ReleaseDelaySeconds, base.PreferredWaitSeconds = &zero, &zero
	explicitZero, err := manager.Create(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if explicitZero.ReleaseDelaySeconds != 0 || explicitZero.PreferredWaitSeconds != 0 {
		t.Fatalf("explicit zero delays = %d, %d", explicitZero.ReleaseDelaySeconds, explicitZero.PreferredWaitSeconds)
	}
	negative := int64(-1)
	base.ReleaseDelaySeconds = &negative
	if _, err := manager.Create(context.Background(), base); err == nil {
		t.Fatal("negative release delay was accepted")
	} else {
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("negative delay error = %T %v, want ValidationError", err, err)
		}
	}
}

func TestManagerSchedulesKnownEpisodeAtReleaseTime(t *testing.T) {
	manager, store, _, watchID := newManagerFixture(t, FallbackStrict, "Some.Show.S01E02.1080p.WEB-DL.x265-MeGusta.mkv", 0)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	air := now.Add(12 * time.Hour)
	number := 2
	manager.TVMaze = &fakeTVMaze{episodes: []TVMazeEpisode{{ID: 1002, Name: "Second", Season: 1, Number: &number, Airstamp: &air}}}

	if err := manager.processWatch(context.Background(), watchID); err != nil {
		t.Fatal(err)
	}
	watch, err := store.GetWatch(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	want := air.Add(time.Second)
	got, ok := parseNullTime(watch.NextCheckAt)
	if !ok || !got.Equal(want) {
		t.Fatalf("next check = %v; want release search time %v", got, want)
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
	if _, err := manager.Update(context.Background(), watchID, UpdateRequest{ReleaseDelaySeconds: &zero}); err != nil {
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
	if err := manager.queueSelection(ctx, watch, ep); err != nil {
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
