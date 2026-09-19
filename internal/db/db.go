package db

import (
	"context"
	"database/sql"
	"fmt"
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
  organize_by_season INTEGER NOT NULL DEFAULT 1,
  quality_profile_json TEXT NOT NULL DEFAULT '{}',
  start_mode TEXT NOT NULL DEFAULT 'template',
  start_season INTEGER,
  start_episode INTEGER,
  fallback_policy TEXT NOT NULL DEFAULT 'strict',
  release_delay_seconds INTEGER NOT NULL DEFAULT 21600,
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
  job_id INTEGER,
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
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_source_key ON jobs(source_key) WHERE source_key IS NOT NULL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureTableColumn(ctx, db, "series_watches", "series_folder", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
	return db, nil
}

func ensureColumn(ctx context.Context, db *sql.DB, name, colType string) error {
	return ensureTableColumn(ctx, db, "jobs", name, colType)
}

func ensureTableColumn(ctx context.Context, db *sql.DB, table, name, colType string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasCol := false
	for rows.Next() {
		var cid int
		var colName string
		var ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &colName, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if colName == name {
			hasCol = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasCol {
		_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+colType)
		return err
	}
	return nil
}
