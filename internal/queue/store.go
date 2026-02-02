package queue

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	StatusQueued      = "queued"
	StatusResolving   = "resolving"
	StatusDownloading = "downloading"
	StatusPaused      = "paused"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusDeleted     = "deleted"
)

type Job struct {
	ID            int64
	URL           string
	Site          string
	OutDir        string
	Name          string
	ResolvedURL   sql.NullString
	Filename      sql.NullString
	SizeBytes     sql.NullInt64
	BytesDone     int64
	DownloadSpeed sql.NullInt64
	EtaSeconds    sql.NullInt64
	Status        string
	Error         sql.NullString
	ErrorCode     sql.NullString
	Engine        string
	EngineGID     sql.NullString
	Attempts      int
	MaxAttempts   int
	NextRetryAt   sql.NullString
	CreatedAt     string
	UpdatedAt     string
	StartedAt     sql.NullString
	CompletedAt   sql.NullString
	DeletedAt     sql.NullString
}

// Store wraps DB access for jobs and events.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateJob(ctx context.Context, j *Job) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (url, site, out_dir, name, status, created_at, updated_at, max_attempts)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, j.URL, j.Site, j.OutDir, j.Name, StatusQueued, now, now, j.MaxAttempts)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) GetJob(ctx context.Context, id int64) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, url, site, out_dir, name, resolved_url, filename, size_bytes, bytes_done, download_speed, eta_seconds, status, error, error_code,
       engine, engine_gid, attempts, max_attempts, next_retry_at, created_at, updated_at, started_at, completed_at, deleted_at
FROM jobs WHERE id = ?
`, id)
	var j Job
	err := row.Scan(
		&j.ID, &j.URL, &j.Site, &j.OutDir, &j.Name, &j.ResolvedURL, &j.Filename, &j.SizeBytes, &j.BytesDone, &j.DownloadSpeed, &j.EtaSeconds,
		&j.Status, &j.Error, &j.ErrorCode, &j.Engine, &j.EngineGID, &j.Attempts, &j.MaxAttempts,
		&j.NextRetryAt, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt, &j.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) ListJobs(ctx context.Context, status string, includeDeleted bool) ([]Job, error) {
	query := `
SELECT id, url, site, out_dir, name, resolved_url, filename, size_bytes, bytes_done, download_speed, eta_seconds, status, error, error_code,
       engine, engine_gid, attempts, max_attempts, next_retry_at, created_at, updated_at, started_at, completed_at, deleted_at
FROM jobs`
	args := []any{}
	where := []string{}
	if !includeDeleted {
		where = append(where, "deleted_at IS NULL")
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(
			&j.ID, &j.URL, &j.Site, &j.OutDir, &j.Name, &j.ResolvedURL, &j.Filename, &j.SizeBytes, &j.BytesDone, &j.DownloadSpeed, &j.EtaSeconds,
			&j.Status, &j.Error, &j.ErrorCode, &j.Engine, &j.EngineGID, &j.Attempts, &j.MaxAttempts,
			&j.NextRetryAt, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt, &j.DeletedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) AddEvent(ctx context.Context, jobID int64, level, msg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO job_events (job_id, level, message, created_at) VALUES (?, ?, ?, ?)
`, jobID, level, msg, now)
	return err
}

func (s *Store) ListEvents(ctx context.Context, jobID int64, limit int) ([]string, error) {
	query := `SELECT created_at || ' ' || level || ' ' || message FROM job_events WHERE job_id = ? ORDER BY id DESC`
	args := []any{jobID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}

// ClaimNextQueued finds a queued job ready to run and marks it as resolving.
func (s *Store) ClaimNextQueued(ctx context.Context) (*Job, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	// Use a transaction for a simple claim.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, url, site, out_dir, name, resolved_url, filename, size_bytes, bytes_done, status, error, error_code,
       download_speed, eta_seconds, engine, engine_gid, attempts, max_attempts, next_retry_at, created_at, updated_at, started_at, completed_at, deleted_at
FROM jobs
WHERE status = ? AND deleted_at IS NULL AND (next_retry_at IS NULL OR next_retry_at <= ?)
ORDER BY id ASC
LIMIT 1
`, StatusQueued, now)
	var j Job
	if err := row.Scan(
		&j.ID, &j.URL, &j.Site, &j.OutDir, &j.Name, &j.ResolvedURL, &j.Filename, &j.SizeBytes, &j.BytesDone,
		&j.Status, &j.Error, &j.ErrorCode, &j.DownloadSpeed, &j.EtaSeconds, &j.Engine, &j.EngineGID, &j.Attempts, &j.MaxAttempts,
		&j.NextRetryAt, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt, &j.DeletedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE jobs SET status = ?, updated_at = ?, started_at = ? WHERE id = ?
`, StatusResolving, now, now, j.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	j.Status = StatusResolving
	return &j, nil
}

func (s *Store) UpdateResolving(ctx context.Context, id int64, resolvedURL, filename string, sizeBytes int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs SET resolved_url = ?, filename = ?, size_bytes = ?, updated_at = ? WHERE id = ?
`, resolvedURL, filename, sizeBytes, now, id)
	return err
}

func (s *Store) MarkDownloading(ctx context.Context, id int64, engine, gid string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs SET status = ?, engine = ?, engine_gid = ?, updated_at = ? WHERE id = ?
`, StatusDownloading, engine, gid, now, id)
	return err
}

func (s *Store) MarkPaused(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?
`, StatusPaused, now, id)
	return err
}

func (s *Store) MarkDownloadingStatus(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?
`, StatusDownloading, now, id)
	return err
}

func (s *Store) UpdateProgress(ctx context.Context, id int64, bytesDone int64, status string, speed int64, etaSeconds int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs SET bytes_done = ?, status = ?, download_speed = ?, eta_seconds = ?, updated_at = ? WHERE id = ?
`, bytesDone, status, speed, etaSeconds, now, id)
	return err
}

func (s *Store) MarkCompleted(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET status = ?,
    bytes_done = CASE
      WHEN bytes_done = 0 AND size_bytes IS NOT NULL THEN size_bytes
      ELSE bytes_done
    END,
    download_speed = 0,
    eta_seconds = NULL,
    updated_at = ?,
    completed_at = ?
WHERE id = ?
`, StatusCompleted, now, now, id)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id int64, code, msg string, nextRetry time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var retryStr any = nil
	if !nextRetry.IsZero() {
		retryStr = nextRetry.UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET status = ?, error = ?, error_code = ?, download_speed = 0, eta_seconds = NULL, updated_at = ?, next_retry_at = ?, attempts = attempts + 1
WHERE id = ?
`, StatusFailed, msg, code, now, retryStr, id)
	return err
}

func (s *Store) Requeue(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET status = ?,
    error = NULL,
    error_code = NULL,
    next_retry_at = NULL,
    deleted_at = NULL,
    resolved_url = NULL,
    filename = NULL,
    size_bytes = NULL,
    download_speed = 0,
    eta_seconds = NULL,
    engine = 'aria2',
    engine_gid = NULL,
    started_at = NULL,
    completed_at = NULL,
    updated_at = ?
WHERE id = ?
`, StatusQueued, now, id)
	return err
}

func (s *Store) Remove(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs SET status = ?, deleted_at = ?, updated_at = ?
WHERE id = ?
`, StatusDeleted, now, now, id)
	return err
}

func (s *Store) ClearAll(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM job_events`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs`); err != nil {
		return err
	}
	var seqName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='sqlite_sequence'`).Scan(&seqName); err == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name IN ('jobs', 'job_events')`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
