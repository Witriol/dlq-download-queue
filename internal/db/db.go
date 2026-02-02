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
`

// Open opens the SQLite database and ensures schema exists.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", path)
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
	return db, nil
}

func ensureColumn(ctx context.Context, db *sql.DB, name, colType string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(jobs)`)
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
		_, err = db.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN `+name+` `+colType)
		return err
	}
	return nil
}
