package series

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Witriol/dlq-download-queue/internal/db"
)

func newSeriesStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewStore(conn), conn
}

func TestEpisodeUpsertPreservesSelectionAndJob(t *testing.T) {
	store, conn := newSeriesStore(t)
	ctx := context.Background()
	watchID, err := store.CreateWatch(ctx, &Watch{
		Enabled: true, TVMazeID: 42, DisplayName: "Example", SearchTitle: "Example",
		ReferenceWebshareIdent: "abc", ReferenceFilename: "Example.S01E01.mkv", OutDir: "/data",
	})
	if err != nil {
		t.Fatalf("create watch: %v", err)
	}
	ep, err := store.UpsertEpisode(ctx, EpisodeInput{WatchID: watchID, TVMazeEpisodeID: 99, Season: 1, Episode: 2, EpisodeName: "First", State: StateSearching})
	if err != nil {
		t.Fatalf("upsert episode: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `INSERT INTO jobs (url, out_dir, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "https://example.invalid/file", "/data", "queued", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)).Err(); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	var jobID int64
	if err := conn.QueryRowContext(ctx, `SELECT id FROM jobs ORDER BY id DESC LIMIT 1`).Scan(&jobID); err != nil {
		t.Fatalf("get job id: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE series_episodes SET chosen_webshare_ident = ?, chosen_filename = ?, state = ? WHERE id = ?`, "chosen", "Example.S01E02.mkv", StateQueued, ep.ID); err != nil {
		t.Fatalf("set selection: %v", err)
	}
	if err := store.AttachJob(ctx, ep.ID, jobID, StateQueued); err != nil {
		t.Fatalf("attach job: %v", err)
	}
	updated, err := store.UpsertEpisode(ctx, EpisodeInput{WatchID: watchID, TVMazeEpisodeID: 99, Season: 1, Episode: 2, EpisodeName: "Renamed", State: StateScheduled})
	if err != nil {
		t.Fatalf("repeat upsert: %v", err)
	}
	if updated.ID != ep.ID || updated.State != StateQueued || !updated.JobID.Valid || updated.JobID.Int64 != jobID || !updated.ChosenWebshareIdent.Valid {
		t.Fatalf("repeat upsert overwrote durable state: %+v", updated)
	}
	if updated.EpisodeName != "Renamed" {
		t.Fatalf("expected metadata refresh, got %q", updated.EpisodeName)
	}
}

func TestAttachJobIsIdempotent(t *testing.T) {
	store, conn := newSeriesStore(t)
	ctx := context.Background()
	watchID, err := store.CreateWatch(ctx, &Watch{Enabled: true, TVMazeID: 1, DisplayName: "x", SearchTitle: "x", ReferenceWebshareIdent: "r", ReferenceFilename: "r.mkv", OutDir: "/data"})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := store.UpsertEpisode(ctx, EpisodeInput{WatchID: watchID, TVMazeEpisodeID: 2, Season: 1, Episode: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO jobs (url, out_dir, status, created_at, updated_at) VALUES ('u', '/data', 'queued', 'now', 'now'), ('v', '/data', 'queued', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	if err := store.AttachJob(ctx, ep.ID, 1, StateQueued); err != nil {
		t.Fatal(err)
	}
	if err := store.AttachJob(ctx, ep.ID, 2, StateQueued); err == nil {
		t.Fatal("expected second attach to be rejected")
	}
}

func TestWatchPersistsOutputOrganization(t *testing.T) {
	store, _ := newSeriesStore(t)
	ctx := context.Background()
	id, err := store.CreateWatch(ctx, &Watch{
		Enabled: true, TVMazeID: 1, DisplayName: "Futurama", SearchTitle: "Futurama",
		ReferenceWebshareIdent: "ref", ReferenceFilename: "Futurama.S14E08.mkv",
		OutDir: "/tvshows", SeriesFolder: "futurama", OrganizeBySeason: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	watch, err := store.GetWatch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if watch.OutDir != "/tvshows" || watch.SeriesFolder != "futurama" || !watch.OrganizeBySeason {
		t.Fatalf("persisted watch = %+v", watch)
	}
}

func TestCreateWatchPreservesZeroDelays(t *testing.T) {
	store, _ := newSeriesStore(t)
	watch := &Watch{
		Enabled: true, TVMazeID: 1, DisplayName: "Example", SearchTitle: "Example",
		ReferenceWebshareIdent: "ref", ReferenceFilename: "Example.S01E01.mkv", OutDir: "/data",
		PreferredWaitSeconds: 0,
	}
	if _, err := store.CreateWatch(context.Background(), watch); err != nil {
		t.Fatal(err)
	}
	if watch.PreferredWaitSeconds != 0 {
		t.Fatalf("stored zero preferred wait changed to %d", watch.PreferredWaitSeconds)
	}
}
