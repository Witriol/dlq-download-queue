package db

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;

CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  url TEXT NOT NULL,
  site TEXT,
  out_dir TEXT NOT NULL,
  name TEXT,
  archive_password TEXT,
  resolved_url TEXT,
  filename TEXT,
  size_bytes INTEGER,
  bytes_done INTEGER DEFAULT 0,
  download_speed INTEGER DEFAULT 0,
  eta_seconds INTEGER,
  status TEXT NOT NULL,
  error TEXT,
  error_code TEXT,
  engine TEXT DEFAULT 'aria2',
  engine_gid TEXT,
  attempts INTEGER DEFAULT 0,
  max_attempts INTEGER DEFAULT 5,
  source_key TEXT,
  next_retry_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  status_changed_at TEXT,
  started_at TEXT,
  completed_at TEXT,
  deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_retry ON jobs(next_retry_at);

CREATE TABLE IF NOT EXISTS job_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER NOT NULL,
  level TEXT NOT NULL,
  message TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_job_events_job_id ON job_events(job_id);

CREATE TABLE IF NOT EXISTS series_watches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  enabled INTEGER NOT NULL DEFAULT 1,
  tvmaze_id INTEGER NOT NULL,
  display_name TEXT NOT NULL,
  search_title TEXT NOT NULL,
  reference_webshare_ident TEXT NOT NULL,
  reference_filename TEXT NOT NULL,
  out_dir TEXT NOT NULL,
  series_folder TEXT NOT NULL DEFAULT '',
  output_path_version INTEGER NOT NULL DEFAULT 2,
  organize_by_season INTEGER NOT NULL DEFAULT 1,
  quality_profile_json TEXT NOT NULL DEFAULT '{}',
  start_mode TEXT NOT NULL DEFAULT 'template',
  start_season INTEGER,
  start_episode INTEGER,
  fallback_policy TEXT NOT NULL DEFAULT 'strict',
  preferred_wait_seconds INTEGER NOT NULL DEFAULT 86400,
  next_check_at TEXT,
  last_checked_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_series_watches_due
  ON series_watches(enabled, next_check_at);

CREATE TABLE IF NOT EXISTS series_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  watch_id INTEGER NOT NULL,
  tvmaze_episode_id INTEGER NOT NULL,
  season INTEGER NOT NULL,
  episode INTEGER NOT NULL,
  episode_name TEXT NOT NULL DEFAULT '',
  air_timestamp TEXT,
  state TEXT NOT NULL DEFAULT 'scheduled',
  chosen_webshare_ident TEXT,
  chosen_filename TEXT,
  selection_snapshot_json TEXT,
  attention_candidates_json TEXT,
  search_attempts INTEGER NOT NULL DEFAULT 0,
  job_id INTEGER,
  runtime_minutes INTEGER,
  search_started_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(watch_id) REFERENCES series_watches(id) ON DELETE CASCADE,
  FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE SET NULL,
  UNIQUE(watch_id, tvmaze_episode_id),
  UNIQUE(watch_id, chosen_webshare_ident)
);

CREATE INDEX IF NOT EXISTS idx_series_episodes_watch_state
  ON series_episodes(watch_id, state);
CREATE INDEX IF NOT EXISTS idx_series_episodes_job_id
  ON series_episodes(job_id);

CREATE TABLE IF NOT EXISTS watch_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  watch_id INTEGER NOT NULL,
  episode_id INTEGER,
  level TEXT NOT NULL,
  message TEXT NOT NULL,
  details_json TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(watch_id) REFERENCES series_watches(id) ON DELETE CASCADE,
  FOREIGN KEY(episode_id) REFERENCES series_episodes(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_watch_events_watch_id
  ON watch_events(watch_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_watch_events_episode_id
  ON watch_events(episode_id, id DESC);
`

// Open opens the SQLite database and ensures schema exists.
func Open(path string) (*sql.DB, error) {
	// busy_timeout is connection-local in SQLite, so keep it in the DSN rather
	// than executing a PRAGMA once on an arbitrary pooled connection. Watch
	// checks can fetch in parallel but still briefly serialize their writes.
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(ctx, db, "deleted_at", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(ctx, db, "download_speed", "INTEGER DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(ctx, db, "eta_seconds", "INTEGER"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(ctx, db, "archive_password", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(ctx, db, "source_key", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(ctx, db, "status_changed_at", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET status_changed_at = COALESCE(status_changed_at, deleted_at, completed_at, updated_at, created_at)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
CREATE TRIGGER IF NOT EXISTS trg_jobs_status_changed_at
AFTER UPDATE OF status ON jobs
WHEN OLD.status IS NOT NEW.status
BEGIN
  UPDATE jobs SET status_changed_at = COALESCE(NEW.updated_at, CURRENT_TIMESTAMP) WHERE id = NEW.id;
END`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_source_key ON jobs(source_key) WHERE source_key IS NOT NULL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_watches", "series_folder", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_watches", "output_path_version", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateSeriesOutputPaths(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_watches", "organize_by_season", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_episodes", "attention_candidates_json", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_episodes", "search_attempts", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_episodes", "runtime_minutes", "INTEGER"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_episodes", "search_started_at", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_watches", "show_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, err
	}
	// NULL air_timezone means the show was never fetched; '' means TVmaze
	// has no channel timezone.
	if err := ensureTableColumn(ctx, db, "series_watches", "air_timezone", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_episodes", "air_date", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_episodes", "airtime_known", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := dropTableColumn(ctx, db, "series_watches", "release_delay_seconds"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// migrateSeriesOutputPaths converts the original root + series_folder model
// to the v2 contract where out_dir is the exact user-selected series folder.
func migrateSeriesOutputPaths(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, out_dir, series_folder FROM series_watches WHERE output_path_version < 2`)
	if err != nil {
		return err
	}
	type watchPath struct {
		id                int64
		outDir, subfolder string
	}
	var watches []watchPath
	for rows.Next() {
		var item watchPath
		if err := rows.Scan(&item.id, &item.outDir, &item.subfolder); err != nil {
			rows.Close()
			return err
		}
		watches = append(watches, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, watch := range watches {
		outDir := strings.TrimSpace(watch.outDir)
		if folder := strings.TrimSpace(watch.subfolder); folder != "" {
			outDir = path.Join(outDir, folder)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE series_watches SET out_dir = ?, series_folder = '', output_path_version = 2 WHERE id = ?`, outDir, watch.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureColumn(ctx context.Context, db *sql.DB, name, colType string) error {
	return ensureTableColumn(ctx, db, "jobs", name, colType)
}

func ensureTableColumn(ctx context.Context, db *sql.DB, table, name, colType string) error {
	hasCol, err := tableHasColumn(ctx, db, table, name)
	if err != nil || hasCol {
		return err
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+colType)
	return err
}

func dropTableColumn(ctx context.Context, db *sql.DB, table, name string) error {
	hasCol, err := tableHasColumn(ctx, db, table, name)
	if err != nil || !hasCol {
		return err
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` DROP COLUMN `+name)
	return err
}

func tableHasColumn(ctx context.Context, db *sql.DB, table, name string) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var colName string
		var ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &colName, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if colName == name {
			return true, nil
		}
	}
	return false, rows.Err()
}
