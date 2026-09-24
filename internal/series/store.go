package series

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Store persists series watches independently from the download queue.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateWatch(ctx context.Context, w *Watch) (int64, error) {
	if w == nil {
		return 0, errors.New("nil series watch")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	quality := w.QualityProfileJSON
	if strings.TrimSpace(quality) == "" {
		quality = "{}"
	}
	startMode := w.StartMode
	if startMode == "" {
		startMode = StartModeTemplate
	}
	policy := w.FallbackPolicy
	if policy == "" {
		policy = FallbackStrict
	}
	if w.PreferredWaitSeconds < 0 {
		return 0, invalid("preferred_wait_seconds must not be negative")
	}
	preferredWait := w.PreferredWaitSeconds
	// A newly created watch is active by default; callers can explicitly pause
	// it immediately with SetWatchEnabled when creation is staged.
	enabled := w.Enabled
	if !enabled {
		enabled = true
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO series_watches (
  enabled, tvmaze_id, display_name, search_title, reference_webshare_ident,
  reference_filename, out_dir, series_folder, output_path_version, organize_by_season,
  quality_profile_json, start_mode, start_season,
  start_episode, fallback_policy, preferred_wait_seconds,
  next_check_at, last_checked_at, last_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 2, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, boolInt(enabled), w.TVMazeID, w.DisplayName, w.SearchTitle,
		w.ReferenceWebshareIdent, w.ReferenceFilename, w.OutDir, w.SeriesFolder,
		boolInt(w.OrganizeBySeason), quality, startMode,
		nullInt64Value(w.StartSeason), nullInt64Value(w.StartEpisode), policy,
		preferredWait, nullStringValue(w.NextCheckAt),
		nullStringValue(w.LastCheckedAt), nullStringValue(w.LastError), now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	w.ID = id
	w.Enabled = enabled
	w.QualityProfileJSON = quality
	w.StartMode = startMode
	w.FallbackPolicy = policy
	w.PreferredWaitSeconds = preferredWait
	w.CreatedAt, w.UpdatedAt = now, now
	return id, nil
}

func (s *Store) GetWatch(ctx context.Context, id int64) (*Watch, error) {
	row := s.db.QueryRowContext(ctx, watchSelect+` WHERE id = ?`, id)
	w, err := scanWatch(row)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Store) ListWatches(ctx context.Context, enabledOnly bool) ([]Watch, error) {
	query := watchSelect
	args := []any{}
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWatches(rows)
}

// ListDueWatches returns enabled watches whose check time is due. A NULL
// next_check_at is intentionally considered due so newly-created watches are
// picked up without a separate initialization write.
func (s *Store) ListDueWatches(ctx context.Context, now time.Time, limit int) ([]Watch, error) {
	query := watchSelect + ` WHERE enabled = 1 AND (next_check_at IS NULL OR next_check_at <= ?) ORDER BY COALESCE(next_check_at, '') ASC, id ASC`
	args := []any{now.UTC().Format(time.RFC3339Nano)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWatches(rows)
}

func (s *Store) UpdateWatchCheck(ctx context.Context, id int64, lastCheckedAt, nextCheckAt *time.Time, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var last, next any
	if lastCheckedAt != nil {
		last = lastCheckedAt.UTC().Format(time.RFC3339Nano)
	}
	if nextCheckAt != nil {
		next = nextCheckAt.UTC().Format(time.RFC3339Nano)
	}
	var errValue any
	if strings.TrimSpace(lastError) != "" {
		errValue = lastError
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_watches SET last_checked_at = ?, next_check_at = ?, last_error = ?, updated_at = ? WHERE id = ?
`, last, next, errValue, now, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ScheduleWatchCheck updates only the due time, preserving the last completed
// check and its diagnostic state.
func (s *Store) ScheduleWatchCheck(ctx context.Context, id int64, next time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE series_watches SET next_check_at = ?, updated_at = ? WHERE id = ?`,
		next.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// SetWatchShow stores the TVmaze show status as returned by TVmaze and the
// channel timezone ("" when TVmaze has none).
func (s *Store) SetWatchShow(ctx context.Context, id int64, status, airTimezone string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE series_watches SET show_status = ?, air_timezone = ?, updated_at = ? WHERE id = ?`, status, airTimezone, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// SetWatchEnabled changes whether a watch is picked up by ListDueWatches.
func (s *Store) SetWatchEnabled(ctx context.Context, id int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE series_watches SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpdateWatch replaces the user-editable watch settings while preserving
// scheduler timestamps and audit history.
func (s *Store) UpdateWatch(ctx context.Context, w *Watch) error {
	if w == nil || w.ID <= 0 {
		return errors.New("series watch id is required")
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_watches SET
  tvmaze_id = ?, display_name = ?, search_title = ?, reference_webshare_ident = ?,
  reference_filename = ?, out_dir = ?, series_folder = ?, organize_by_season = ?,
  quality_profile_json = ?, start_mode = ?,
  start_season = ?, start_episode = ?, fallback_policy = ?,
  preferred_wait_seconds = ?, updated_at = ?
WHERE id = ?
`, w.TVMazeID, w.DisplayName, w.SearchTitle, w.ReferenceWebshareIdent,
		w.ReferenceFilename, w.OutDir, w.SeriesFolder, boolInt(w.OrganizeBySeason),
		w.QualityProfileJSON, w.StartMode,
		nullInt64Value(w.StartSeason), nullInt64Value(w.StartEpisode), w.FallbackPolicy,
		w.PreferredWaitSeconds, time.Now().UTC().Format(time.RFC3339Nano), w.ID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (s *Store) DeleteWatch(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM series_watches WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpsertEpisode inserts a TVmaze episode or refreshes its schedule metadata.
// Existing state, selected candidate, and queue job fields are deliberately
// preserved, making repeated TVmaze syncs safe after a restart. updated_at
// moves only when metadata changes: the UI reads a completed episode's
// updated_at as its download-finished time.
func (s *Store) UpsertEpisode(ctx context.Context, in EpisodeInput) (*Episode, error) {
	if in.WatchID <= 0 || in.TVMazeEpisodeID <= 0 {
		return nil, errors.New("watch_id and tvmaze_episode_id are required")
	}
	state := in.State
	if state == "" {
		state = StateScheduled
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var air any
	if in.AirTimestamp != nil {
		air = in.AirTimestamp.UTC().Format(time.RFC3339Nano)
	}
	var runtime any
	if in.RuntimeMinutes != nil {
		runtime = *in.RuntimeMinutes
	}
	// A NULL stored air_date is a row from before air dates were kept; filling
	// it in is a backfill, not a metadata change.
	_, err := s.db.ExecContext(ctx, `
INSERT INTO series_episodes (
  watch_id, tvmaze_episode_id, season, episode, episode_name, air_timestamp,
  air_date, airtime_known, runtime_minutes, state, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(watch_id, tvmaze_episode_id) DO UPDATE SET
  season = excluded.season,
  episode = excluded.episode,
  episode_name = excluded.episode_name,
  air_timestamp = excluded.air_timestamp,
  air_date = excluded.air_date,
  airtime_known = excluded.airtime_known,
  runtime_minutes = excluded.runtime_minutes,
  updated_at = CASE WHEN season IS NOT excluded.season
      OR episode IS NOT excluded.episode
      OR episode_name IS NOT excluded.episode_name
      OR air_timestamp IS NOT excluded.air_timestamp
      OR runtime_minutes IS NOT excluded.runtime_minutes
      OR (air_date IS NOT NULL AND (air_date IS NOT excluded.air_date
        OR airtime_known IS NOT excluded.airtime_known))
    THEN excluded.updated_at ELSE updated_at END
`, in.WatchID, in.TVMazeEpisodeID, in.Season, in.Episode, in.EpisodeName, air,
		nullOrValue(in.AirDate), boolInt(in.AirtimeKnown), runtime, state, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetEpisodeByTVMazeID(ctx, in.WatchID, in.TVMazeEpisodeID)
}

func (s *Store) GetEpisode(ctx context.Context, id int64) (*Episode, error) {
	row := s.db.QueryRowContext(ctx, episodeSelect+` WHERE id = ?`, id)
	e, err := scanEpisode(row)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) GetEpisodeByTVMazeID(ctx context.Context, watchID, tvmazeEpisodeID int64) (*Episode, error) {
	row := s.db.QueryRowContext(ctx, episodeSelect+` WHERE watch_id = ? AND tvmaze_episode_id = ?`, watchID, tvmazeEpisodeID)
	e, err := scanEpisode(row)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListEpisodes(ctx context.Context, watchID int64) ([]Episode, error) {
	rows, err := s.db.QueryContext(ctx, episodeSelect+` WHERE watch_id = ? ORDER BY season ASC, episode ASC, id ASC`, watchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

// ListUnfinishedJobEpisodes returns episodes of every watch whose queue job
// has not reached a final episode state.
func (s *Store) ListUnfinishedJobEpisodes(ctx context.Context) ([]Episode, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(finalEpisodeStates)), ", ")
	args := make([]any, 0, len(finalEpisodeStates))
	for _, state := range finalEpisodeStates {
		args = append(args, state)
	}
	rows, err := s.db.QueryContext(ctx, episodeSelect+` WHERE job_id IS NOT NULL AND state NOT IN (`+placeholders+`) ORDER BY watch_id ASC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) SetEpisodeState(ctx context.Context, id int64, state string) error {
	if strings.TrimSpace(state) == "" {
		return errors.New("episode state is required")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE series_episodes SET state = ?, updated_at = ? WHERE id = ?`, state, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// StartEpisodeSearch records the start of an episode's search window once.
// It returns the stored start, which is earlier than now on later searches.
func (s *Store) StartEpisodeSearch(ctx context.Context, id int64, now time.Time) (time.Time, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE series_episodes SET search_started_at = COALESCE(search_started_at, ?), updated_at = ? WHERE id = ?`,
		now.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return time.Time{}, err
	}
	if err := requireAffected(res); err != nil {
		return time.Time{}, err
	}
	var started sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT search_started_at FROM series_episodes WHERE id = ?`, id).Scan(&started); err != nil {
		return time.Time{}, err
	}
	t, ok := parseNullTime(started)
	if !ok {
		return time.Time{}, errors.New("invalid search_started_at")
	}
	return t, nil
}

// IncrementEpisodeSearchAttempts durably records an unsuccessful release
// search so restarts cannot reset the bounded retry lifecycle.
func (s *Store) IncrementEpisodeSearchAttempts(ctx context.Context, id int64) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE series_episodes SET search_attempts = search_attempts + 1, updated_at = ? WHERE id = ? AND job_id IS NULL`, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return 0, err
	}
	if err := requireAffected(res); err != nil {
		return 0, err
	}
	var attempts int
	if err := s.db.QueryRowContext(ctx, `SELECT search_attempts FROM series_episodes WHERE id = ?`, id).Scan(&attempts); err != nil {
		return 0, err
	}
	return attempts, nil
}

// SetEpisodeAttention stores the current accepted alternatives alongside an
// attention state. Candidates are a server-generated snapshot, never request
// input, so later manual selection can be validated without trusting a URL or
// filename from the client.
func (s *Store) SetEpisodeAttention(ctx context.Context, id int64, candidatesJSON string) error {
	var candidates any
	if strings.TrimSpace(candidatesJSON) != "" {
		candidates = candidatesJSON
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_episodes SET state = ?, attention_candidates_json = ?, updated_at = ?
WHERE id = ? AND job_id IS NULL
`, StateNeedsAttention, candidates, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ResetExhaustedEpisodes makes automatically managed episodes eligible for a
// fresh release-search lifecycle. It deliberately refuses to touch a durable
// selection or queue job; those records are the idempotency boundary for a
// download already chosen by the user or scheduler.
func (s *Store) ResetExhaustedEpisodes(ctx context.Context, watchID int64) (int64, error) {
	if watchID <= 0 {
		return 0, errors.New("watch_id is required")
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_episodes
SET state = ?, search_attempts = 0, search_started_at = NULL, attention_candidates_json = NULL, updated_at = ?
WHERE watch_id = ? AND state = ? AND job_id IS NULL AND chosen_webshare_ident IS NULL
`, StateScheduled, time.Now().UTC().Format(time.RFC3339Nano), watchID, StateNeedsAttention)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetEpisodeSelection records the stable Webshare identity chosen by the
// scheduler before it creates a queue job. The unique constraint on
// (watch_id, chosen_webshare_ident) prevents the same release being selected
// twice for one watch.
func (s *Store) SetEpisodeSelection(ctx context.Context, id int64, webshareIdent, filename, snapshotJSON string) error {
	if strings.TrimSpace(webshareIdent) == "" {
		return errors.New("webshare identifier is required")
	}
	var snapshot any
	if strings.TrimSpace(snapshotJSON) != "" {
		snapshot = snapshotJSON
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_episodes SET chosen_webshare_ident = ?, chosen_filename = ?, selection_snapshot_json = ?, state = ?, updated_at = ?
WHERE id = ? AND chosen_webshare_ident IS NULL AND job_id IS NULL
`, webshareIdent, nullOrValue(filename), snapshot, StateSearching, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// SelectAttentionCandidate turns a validated attention candidate into the
// durable selection. The caller must validate the snapshot first; the query
// additionally guards the episode/watch relationship and state transition.
func (s *Store) SelectAttentionCandidate(ctx context.Context, watchID, episodeID int64, webshareIdent, filename, snapshotJSON string) error {
	if watchID <= 0 || episodeID <= 0 || strings.TrimSpace(webshareIdent) == "" {
		return errors.New("watch_id, episode_id, and webshare identifier are required")
	}
	var snapshot any
	if strings.TrimSpace(snapshotJSON) != "" {
		snapshot = snapshotJSON
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_episodes
SET chosen_webshare_ident = ?, chosen_filename = ?, selection_snapshot_json = ?,
    attention_candidates_json = NULL, state = ?, updated_at = ?
WHERE id = ? AND watch_id = ? AND state = ?
  AND chosen_webshare_ident IS NULL AND job_id IS NULL
`, webshareIdent, nullOrValue(filename), snapshot, StateSearching,
		time.Now().UTC().Format(time.RFC3339Nano), episodeID, watchID, StateNeedsAttention)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// AttachJob atomically associates an episode with a queue job. It refuses to
// overwrite an existing association, which is the key idempotency guard when
// scheduler ticks overlap.
func (s *Store) AttachJob(ctx context.Context, episodeID, jobID int64, state string) error {
	if episodeID <= 0 || jobID <= 0 {
		return errors.New("episode_id and job_id are required")
	}
	if state == "" {
		state = StateQueued
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE series_episodes SET job_id = ?, state = ?, updated_at = ?
WHERE id = ? AND job_id IS NULL
`, jobID, state, time.Now().UTC().Format(time.RFC3339Nano), episodeID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (s *Store) AddEvent(ctx context.Context, watchID, episodeID int64, level, message, detailsJSON string) (int64, error) {
	if watchID <= 0 || strings.TrimSpace(level) == "" || strings.TrimSpace(message) == "" {
		return 0, errors.New("watch_id, level, and message are required")
	}
	var ep any
	if episodeID > 0 {
		ep = episodeID
	}
	var details any
	if strings.TrimSpace(detailsJSON) != "" {
		details = detailsJSON
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO watch_events (watch_id, episode_id, level, message, details_json, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, watchID, ep, level, message, details, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// PruneEvents keeps only the newest keep events of a watch.
func (s *Store) PruneEvents(ctx context.Context, watchID int64, keep int) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM watch_events
WHERE watch_id = ? AND id NOT IN (
  SELECT id FROM watch_events WHERE watch_id = ? ORDER BY id DESC LIMIT ?
)`, watchID, watchID, keep)
	return err
}

func (s *Store) ListEvents(ctx context.Context, watchID int64, limit int) ([]WatchEvent, error) {
	return s.listEvents(ctx, watchID, 0, limit)
}

func (s *Store) ListEpisodeEvents(ctx context.Context, episodeID int64, limit int) ([]WatchEvent, error) {
	return s.listEvents(ctx, 0, episodeID, limit)
}

func (s *Store) listEvents(ctx context.Context, watchID, episodeID int64, limit int) ([]WatchEvent, error) {
	query := `SELECT id, watch_id, episode_id, level, message, details_json, created_at FROM watch_events`
	args := []any{}
	if watchID > 0 {
		query += ` WHERE watch_id = ?`
		args = append(args, watchID)
	} else if episodeID > 0 {
		query += ` WHERE episode_id = ?`
		args = append(args, episodeID)
	}
	query += ` ORDER BY id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []WatchEvent
	for rows.Next() {
		var e WatchEvent
		if err := rows.Scan(&e.ID, &e.WatchID, &e.EpisodeID, &e.Level, &e.Message, &e.DetailsJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

const watchSelect = `SELECT id, enabled, tvmaze_id, display_name, search_title,
 reference_webshare_ident, reference_filename, out_dir, series_folder,
 organize_by_season, quality_profile_json,
 start_mode, start_season, start_episode, fallback_policy,
 preferred_wait_seconds, next_check_at, last_checked_at, last_error, show_status,
 air_timezone, created_at, updated_at FROM series_watches`

const episodeSelect = `SELECT id, watch_id, tvmaze_episode_id, season, episode,
 episode_name, air_timestamp, air_date, airtime_known, state, chosen_webshare_ident, chosen_filename,
 selection_snapshot_json, attention_candidates_json, search_attempts, job_id,
 runtime_minutes, search_started_at, created_at, updated_at FROM series_episodes`

type scanner interface{ Scan(...any) error }

func scanWatch(row scanner) (Watch, error) {
	var w Watch
	var enabled, organizeBySeason int
	err := row.Scan(&w.ID, &enabled, &w.TVMazeID, &w.DisplayName, &w.SearchTitle,
		&w.ReferenceWebshareIdent, &w.ReferenceFilename, &w.OutDir, &w.SeriesFolder,
		&organizeBySeason, &w.QualityProfileJSON,
		&w.StartMode, &w.StartSeason, &w.StartEpisode, &w.FallbackPolicy,
		&w.PreferredWaitSeconds, &w.NextCheckAt,
		&w.LastCheckedAt, &w.LastError, &w.ShowStatus, &w.AirTimezone, &w.CreatedAt, &w.UpdatedAt)
	w.Enabled = enabled != 0
	w.OrganizeBySeason = organizeBySeason != 0
	return w, err
}

func scanWatches(rows *sql.Rows) ([]Watch, error) {
	var out []Watch
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanEpisode(row scanner) (Episode, error) {
	var e Episode
	var airtimeKnown int
	err := row.Scan(&e.ID, &e.WatchID, &e.TVMazeEpisodeID, &e.Season, &e.Episode,
		&e.EpisodeName, &e.AirTimestamp, &e.AirDate, &airtimeKnown, &e.State, &e.ChosenWebshareIdent,
		&e.ChosenFilename, &e.SelectionSnapshotJSON, &e.AttentionCandidatesJSON,
		&e.SearchAttempts, &e.JobID, &e.RuntimeMinutes, &e.SearchStartedAt,
		&e.CreatedAt, &e.UpdatedAt)
	e.AirtimeKnown = airtimeKnown != 0
	return e, err
}

func scanEpisodes(rows *sql.Rows) ([]Episode, error) {
	var out []Episode
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullStringValue(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func nullInt64Value(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullOrValue(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
