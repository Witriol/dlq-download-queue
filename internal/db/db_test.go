package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMigratesSeriesOutputPathToFinalDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := strings.Replace(schema, "  output_path_version INTEGER NOT NULL DEFAULT 2,\n", "", 1)
	legacySchema = strings.Replace(legacySchema, "  status_changed_at TEXT,\n", "", 1)
	legacySchema = strings.Replace(legacySchema, "  fallback_policy TEXT NOT NULL DEFAULT 'strict',\n", "  fallback_policy TEXT NOT NULL DEFAULT 'strict',\n  release_delay_seconds INTEGER NOT NULL DEFAULT 7200,\n", 1)
	if _, err := legacy.Exec(legacySchema); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
INSERT INTO series_watches (
  enabled, tvmaze_id, display_name, search_title, reference_webshare_ident,
  reference_filename, out_dir, series_folder, organize_by_season,
  quality_profile_json, start_mode, fallback_policy, release_delay_seconds,
  preferred_wait_seconds, created_at, updated_at
) VALUES (1, 1, 'Futurama', 'Futurama', 'ref', 'Futurama.S01E01.mkv',
  '/data/tvshows', 'futurama', 1, '{}', 'template', 'strict', 21600, 86400, 'now', 'now')`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO jobs (url, out_dir, status, created_at, updated_at, completed_at, deleted_at) VALUES ('u', '/data', 'deleted', 'created', 'updated', 'completed', 'deleted')`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var outDir, seriesFolder string
	var version int
	if err := conn.QueryRowContext(context.Background(), `SELECT out_dir, series_folder, output_path_version FROM series_watches WHERE id = 1`).Scan(&outDir, &seriesFolder, &version); err != nil {
		t.Fatal(err)
	}
	if outDir != "/data/tvshows/futurama" || seriesFolder != "" || version != 2 {
		t.Fatalf("migrated path = %q, folder = %q, version = %d", outDir, seriesFolder, version)
	}
	assertNoReleaseDelayColumn(t, conn)
	var changedAt string
	if err := conn.QueryRowContext(context.Background(), `SELECT status_changed_at FROM jobs WHERE id = 1`).Scan(&changedAt); err != nil {
		t.Fatal(err)
	}
	if changedAt != "deleted" {
		t.Fatalf("deleted job status timestamp = %q; want deleted_at", changedAt)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.QueryRowContext(context.Background(), `SELECT out_dir, series_folder, output_path_version FROM series_watches WHERE id = 1`).Scan(&outDir, &seriesFolder, &version); err != nil {
		t.Fatal(err)
	}
	if outDir != "/data/tvshows/futurama" || seriesFolder != "" || version != 2 {
		t.Fatalf("second open changed migrated path = %q, folder = %q, version = %d", outDir, seriesFolder, version)
	}
	assertNoReleaseDelayColumn(t, conn)
}

func TestOpenNewDatabaseHasNoReleaseDelayColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "new.db")
	for i := 0; i < 2; i++ {
		conn, err := Open(dbPath)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		assertNoReleaseDelayColumn(t, conn)
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoReleaseDelayColumn(t *testing.T, conn *sql.DB) {
	t.Helper()
	hasCol, err := tableHasColumn(context.Background(), conn, "series_watches", "release_delay_seconds")
	if err != nil {
		t.Fatal(err)
	}
	if hasCol {
		t.Fatal("series_watches.release_delay_seconds was not dropped")
	}
}
