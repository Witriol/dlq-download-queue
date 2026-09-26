package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Witriol/dlq-download-queue/internal/queue"
	"github.com/Witriol/dlq-download-queue/internal/resolver"
)

const (
	minimumProfileConfidence = 0.55
	maxConcurrentWatchChecks = 4
	defaultEpisodeRuntime    = 60 * time.Minute
	searchGiveUpAfter        = 72 * time.Hour
	minimumCheckInterval     = 5 * time.Minute
	metadataRefreshInterval  = 24 * time.Hour
	endedShowCheckInterval   = 7 * 24 * time.Hour
	tvmazeShowEnded          = "Ended"
	maxEventCandidates       = 10
	maxEventsPerWatch        = 1000
	eventTimeLayout          = "2006-01-02 15:04 UTC"
	tvmazeAirDateLayout      = "2006-01-02"
	queueReasonExact         = "exact"
	queueReasonRetry         = "retry"
	queueReasonManual        = "manual selection"
)

// searchTiers set the release search interval by time since an episode's
// search window started. The window closes at searchGiveUpAfter.
var searchTiers = []struct{ before, every time.Duration }{
	{before: 6 * time.Hour, every: 15 * time.Minute},
	{before: 24 * time.Hour, every: time.Hour},
	{before: searchGiveUpAfter, every: 3 * time.Hour},
}

// finalEpisodeStates end an episode's job lifecycle. decrypt_failed is the
// queue status, which syncJobState passes through unmapped.
var finalEpisodeStates = []string{StateCompleted, StateFailed, StateSkipped, queue.StatusDecryptFail}

var seasonFolderRe = regexp.MustCompile(`(?i)^(s|season[ ._-]?)\d{1,3}$`)

var (
	ErrAttentionSelectionConflict = errors.New("attention selection conflict")
	ErrWatchPaused                = errors.New("watch is paused")
	ErrWatchCheckInProgress       = errors.New("watch check already in progress")
)

type QueueCreator interface {
	CreateJob(ctx context.Context, url, outDir, name, site, archivePassword string, maxAttempts int) (int64, error)
}

// SourceKeyQueueCreator is implemented by queue.Service. The optional
// interface keeps lightweight queue implementations compatible while the
// production queue can make cross-domain retries idempotent.
type SourceKeyQueueCreator interface {
	CreateJobWithSourceKey(ctx context.Context, url, outDir, name, site, archivePassword string, maxAttempts int, sourceKey string) (int64, error)
}

type TVMazeProvider interface {
	SearchShows(context.Context, string) ([]TVMazeSearchResult, error)
	Episodes(context.Context, int64) ([]TVMazeEpisode, error)
	Show(context.Context, int64) (*TVMazeShow, error)
}

type WebshareProvider interface {
	SearchVideos(context.Context, string, int, int) ([]resolver.WebshareSearchResult, error)
	FileInfo(context.Context, string) (*resolver.WebshareFile, error)
}

type CreateRequest struct {
	ReferenceURL           string          `json:"reference_url"`
	ReferenceWebshareURL   string          `json:"reference_webshare_url"`
	ReferenceWebshareIdent string          `json:"reference_webshare_ident"`
	ReferenceFilename      string          `json:"reference_filename"`
	OutDir                 string          `json:"out_dir"`
	SeriesFolder           string          `json:"series_folder"`
	OrganizeBySeason       *bool           `json:"organize_by_season"`
	TVMazeID               int64           `json:"tvmaze_id"`
	DisplayName            string          `json:"display_name"`
	SearchTitle            string          `json:"search_title"`
	StartMode              string          `json:"start_mode"`
	InitialMode            string          `json:"initial_mode"`
	InitialSeason          int             `json:"initial_season"`
	InitialEpisode         int             `json:"initial_episode"`
	FallbackPolicy         string          `json:"fallback_policy"`
	PreferredWaitSeconds   *int64          `json:"preferred_wait_seconds"`
	QualityProfile         json.RawMessage `json:"quality_profile"`
}

type UpdateRequest struct {
	OutDir               *string         `json:"out_dir"`
	SeriesFolder         *string         `json:"series_folder"`
	OrganizeBySeason     *bool           `json:"organize_by_season"`
	SearchTitle          *string         `json:"search_title"`
	FallbackPolicy       *string         `json:"fallback_policy"`
	PreferredWaitSeconds *int64          `json:"preferred_wait_seconds"`
	QualityProfile       json.RawMessage `json:"quality_profile"`
}

type PreviewResponse struct {
	ReferenceWebshareIdent string          `json:"reference_webshare_ident,omitempty"`
	ReferenceFilename      string          `json:"reference_filename,omitempty"`
	SearchTitle            string          `json:"search_title,omitempty"`
	QualityProfile         ReleaseProfile  `json:"quality_profile"`
	Tokens                 []string        `json:"tokens,omitempty"`
	Episode                *EpisodeView    `json:"episode,omitempty"`
	NextEpisode            *EpisodeView    `json:"next_episode,omitempty"`
	TotalCandidates        int             `json:"total_candidates"`
	Candidates             []CandidateView `json:"candidates"`
}

type CandidateView struct {
	Ident         string         `json:"ident"`
	Filename      string         `json:"filename"`
	SizeBytes     int64          `json:"size_bytes"`
	Score         float64        `json:"score"`
	Accepted      bool           `json:"accepted"`
	Exact         bool           `json:"exact"`
	Reasons       []string       `json:"reasons,omitempty"`
	RejectReasons []string       `json:"reject_reasons,omitempty"`
	Profile       ReleaseProfile `json:"profile"`
}

type EpisodeView struct {
	ID              int64  `json:"id"`
	TVMazeEpisodeID int64  `json:"tvmaze_episode_id"`
	Season          int    `json:"season"`
	Episode         int    `json:"episode"`
	EpisodeName     string `json:"episode_name,omitempty"`
	AirTimestamp    string `json:"air_timestamp,omitempty"`
	State           string `json:"state"`
	ChosenFilename  string `json:"chosen_filename,omitempty"`
	SearchAttempts  int    `json:"search_attempts,omitempty"`
	JobID           int64  `json:"job_id,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

// AttentionEpisodeView is intentionally separate from WatchView: candidate
// snapshots are shown only on the manual-review endpoint, not on every poll.
type AttentionEpisodeView struct {
	EpisodeView
	Candidates []CandidateView `json:"candidates"`
}

type SelectAttentionCandidateRequest struct {
	Ident string `json:"ident"`
}

type WatchView struct {
	ID                     int64           `json:"id"`
	Enabled                bool            `json:"enabled"`
	TVMazeID               int64           `json:"tvmaze_id"`
	DisplayName            string          `json:"display_name"`
	SearchTitle            string          `json:"search_title"`
	ReferenceWebshareIdent string          `json:"reference_webshare_ident"`
	ReferenceFilename      string          `json:"reference_filename"`
	OutDir                 string          `json:"out_dir"`
	SeriesFolder           string          `json:"series_folder"`
	OrganizeBySeason       bool            `json:"organize_by_season"`
	QualityProfile         json.RawMessage `json:"quality_profile"`
	FallbackPolicy         string          `json:"fallback_policy"`
	PreferredWaitSeconds   int64           `json:"preferred_wait_seconds"`
	NextCheckAt            string          `json:"next_check_at,omitempty"`
	LastCheckedAt          string          `json:"last_checked_at,omitempty"`
	LastError              string          `json:"last_error,omitempty"`
	ShowStatus             string          `json:"show_status"`
	Status                 string          `json:"status"`
	AttentionCount         int             `json:"attention_count"`
	NextEpisode            *EpisodeView    `json:"next_episode,omitempty"`
	NextSearchAt           string          `json:"next_search_at,omitempty"`
	LastEpisode            *EpisodeView    `json:"last_episode,omitempty"`
	CreatedAt              string          `json:"created_at"`
	UpdatedAt              string          `json:"updated_at"`
}

type storedProfile struct {
	Profile     ReleaseProfile    `json:"profile"`
	Preferences map[string]string `json:"preferences,omitempty"`
}

// JobStatus is the queue state Manager needs from a job. Exported because
// cmd/dlqd/main.go builds it from queue.JobView when wiring Manager.JobState.
type JobStatus struct {
	Status string
	Error  string
}

type Manager struct {
	Store        *Store
	TVMaze       TVMazeProvider
	Webshare     WebshareProvider
	Queue        QueueCreator
	AllowedRoots []string
	Now          func() time.Time
	JobState     func(context.Context, int64) (JobStatus, error)
	runsMu       sync.Mutex
	running      map[int64]struct{}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m *Manager) SearchShows(ctx context.Context, query string) ([]TVMazeSearchResult, error) {
	if m.TVMaze == nil {
		return nil, errors.New("TVmaze client not configured")
	}
	return m.TVMaze.SearchShows(ctx, query)
}

func (m *Manager) Preview(ctx context.Context, req CreateRequest) (*PreviewResponse, error) {
	ident, filename, parsed, err := m.reference(ctx, req)
	if err != nil {
		return nil, err
	}
	profile, preferences, err := decodeProfile(req.QualityProfile, parsed)
	if err != nil {
		return nil, err
	}
	out := &PreviewResponse{ReferenceWebshareIdent: ident, ReferenceFilename: filename, SearchTitle: parsed.SearchTitle, QualityProfile: profile, Tokens: parsed.RawTokens, Candidates: []CandidateView{}}
	if req.TVMazeID > 0 {
		episodes, err := m.TVMaze.Episodes(ctx, req.TVMazeID)
		if err != nil {
			return nil, err
		}
		target := choosePreviewEpisode(episodes, parsed, req, m.now())
		if target != nil && target.Number != nil {
			next := EpisodeView{TVMazeEpisodeID: target.ID, Season: target.Season, Episode: *target.Number, EpisodeName: target.Name}
			if target.Airstamp != nil {
				next.AirTimestamp = target.Airstamp.UTC().Format(time.RFC3339)
			}
			out.NextEpisode = &next
		}
	}
	// The reference episode is known to be released, so searching it shows
	// what the current settings would pick even before the next one airs.
	if len(parsed.Episodes) == 0 {
		return out, nil
	}
	reference := parsed.Episodes[0]
	out.Episode = &EpisodeView{Season: reference.Season, Episode: reference.Episode}
	title := firstNonEmpty(req.SearchTitle, parsed.SearchTitle, req.DisplayName)
	scored, err := m.searchEpisode(ctx, title, reference.Season, reference.Episode, profile, preferences)
	if err != nil {
		return nil, err
	}
	out.TotalCandidates = len(scored)
	for _, c := range scored {
		// A title mismatch is useful for diagnostics and scheduler scoring, but
		// it is noise in the user-facing preview. Keep evaluating all results
		// above and omit only this exact hard-filter reason from the response.
		if hasRejectReason(c, "different series title") {
			continue
		}
		out.Candidates = append(out.Candidates, candidateView(c))
	}
	return out, nil
}

func (m *Manager) Create(ctx context.Context, req CreateRequest) (*WatchView, error) {
	if req.TVMazeID <= 0 {
		return nil, invalid("tvmaze_id is required")
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		return nil, invalid("display_name is required")
	}
	outDir, err := m.cleanOutDir(req.OutDir)
	if err != nil {
		return nil, invalidWrap(err)
	}
	// series_folder is retained as a request compatibility shim for older API
	// clients. New clients select the final series directory directly in
	// out_dir; old root + folder requests are collapsed into that same model.
	if strings.TrimSpace(req.SeriesFolder) != "" {
		seriesFolder, err := normalizeSeriesFolder(req.SeriesFolder)
		if err != nil {
			return nil, invalidWrap(err)
		}
		outDir, err = m.cleanOutDir(filepath.Join(outDir, seriesFolder))
		if err != nil {
			return nil, invalidWrap(err)
		}
	}
	ident, filename, parsed, err := m.reference(ctx, req)
	if err != nil {
		return nil, err
	}
	if parsed.Confidence < minimumProfileConfidence || len(parsed.Episodes) == 0 {
		return nil, invalid("reference filename could not be parsed confidently")
	}
	profile, preferences, err := decodeProfile(req.QualityProfile, parsed)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(storedProfile{Profile: profile, Preferences: preferences})
	mode := firstNonEmpty(req.StartMode, req.InitialMode, StartModeTemplate)
	if !validStartMode(mode) {
		return nil, invalid("invalid start_mode")
	}
	policy := firstNonEmpty(req.FallbackPolicy, FallbackStrict)
	if !validPolicy(policy) {
		return nil, invalid("invalid fallback_policy")
	}
	preferredWait := int64(DefaultPreferredWait / time.Second)
	if req.PreferredWaitSeconds != nil {
		if *req.PreferredWaitSeconds < 0 {
			return nil, invalid("preferred_wait_seconds must not be negative")
		}
		preferredWait = *req.PreferredWaitSeconds
	}
	searchTitle := firstNonEmpty(req.SearchTitle, parsed.SearchTitle, req.DisplayName)
	organizeBySeason := true
	if req.OrganizeBySeason != nil {
		organizeBySeason = *req.OrganizeBySeason
	}
	w := &Watch{Enabled: true, TVMazeID: req.TVMazeID, DisplayName: strings.TrimSpace(req.DisplayName), SearchTitle: searchTitle, ReferenceWebshareIdent: ident, ReferenceFilename: filename, OutDir: outDir, SeriesFolder: "", OrganizeBySeason: organizeBySeason, QualityProfileJSON: string(encoded), StartMode: mode, FallbackPolicy: policy, PreferredWaitSeconds: preferredWait}
	if mode == StartModeSpecific {
		if req.InitialSeason <= 0 || req.InitialEpisode <= 0 {
			return nil, invalid("specific start mode requires initial_season and initial_episode")
		}
		w.StartSeason, w.StartEpisode = sql.NullInt64{Int64: int64(req.InitialSeason), Valid: true}, sql.NullInt64{Int64: int64(req.InitialEpisode), Valid: true}
	} else if mode == StartModeContinue && len(parsed.Episodes) > 0 {
		last := parsed.Episodes[len(parsed.Episodes)-1]
		w.StartSeason, w.StartEpisode = sql.NullInt64{Int64: int64(last.Season), Valid: true}, sql.NullInt64{Int64: int64(last.Episode), Valid: true}
	}
	now := m.now()
	w.NextCheckAt = sql.NullString{String: now.Format(time.RFC3339Nano), Valid: true}
	if _, err := m.Store.CreateWatch(ctx, w); err != nil {
		return nil, err
	}
	m.addEvent(ctx, w.ID, 0, "info", "series watch created", "")
	return m.Get(ctx, w.ID)
}

func (m *Manager) List(ctx context.Context) ([]WatchView, error) {
	watches, err := m.Store.ListWatches(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]WatchView, 0, len(watches))
	for i := range watches {
		v, err := m.view(ctx, &watches[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func (m *Manager) Get(ctx context.Context, id int64) (*WatchView, error) {
	w, err := m.Store.GetWatch(ctx, id)
	if err != nil {
		return nil, err
	}
	return m.view(ctx, w)
}

func (m *Manager) Update(ctx context.Context, id int64, req UpdateRequest) (*WatchView, error) {
	w, err := m.Store.GetWatch(ctx, id)
	if err != nil {
		return nil, err
	}
	before := *w
	originalPolicy := w.FallbackPolicy
	searchSettingsChanged := req.SearchTitle != nil || req.PreferredWaitSeconds != nil || len(req.QualityProfile) > 0
	policyChanged := req.FallbackPolicy != nil && *req.FallbackPolicy != originalPolicy
	if req.OutDir != nil {
		outDir, err := m.cleanOutDir(*req.OutDir)
		if err != nil {
			return nil, invalidWrap(err)
		}
		w.OutDir = outDir
	}
	if req.SeriesFolder != nil {
		// Updates always use the v2 exact out_dir contract. Ignoring this
		// deprecated field also prevents a cached v1 UI from appending the show
		// name again after the database migration has already done so.
		w.SeriesFolder = ""
	}
	if req.OrganizeBySeason != nil {
		w.OrganizeBySeason = *req.OrganizeBySeason
	}
	if req.SearchTitle != nil {
		if strings.TrimSpace(*req.SearchTitle) == "" {
			return nil, invalid("search_title must not be empty")
		}
		w.SearchTitle = strings.TrimSpace(*req.SearchTitle)
	}
	if req.FallbackPolicy != nil {
		if !validPolicy(*req.FallbackPolicy) {
			return nil, invalid("invalid fallback_policy")
		}
		w.FallbackPolicy = *req.FallbackPolicy
	}
	if req.PreferredWaitSeconds != nil {
		if *req.PreferredWaitSeconds < 0 {
			return nil, invalid("preferred_wait_seconds must not be negative")
		}
		w.PreferredWaitSeconds = *req.PreferredWaitSeconds
	}
	if len(req.QualityProfile) > 0 {
		fallback, _, err := profileForWatch(w)
		if err != nil {
			return nil, err
		}
		p, prefs, err := decodeProfile(req.QualityProfile, fallback)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(storedProfile{Profile: p, Preferences: prefs})
		w.QualityProfileJSON = string(b)
	}
	if err := m.Store.UpdateWatch(ctx, w); err != nil {
		return nil, err
	}
	m.logSettingsChanged(ctx, id, before, *w)
	// Strict and balanced watches have no candidate-review action once their
	// bounded search cycle is exhausted. A relevant edit therefore starts a
	// clean cycle. Manual snapshots remain intact unless the user explicitly
	// changes fallback policy, preserving their deliberate review workflow.
	if policyChanged || (searchSettingsChanged && w.FallbackPolicy != FallbackManual) {
		reset, err := m.Store.ResetExhaustedEpisodes(ctx, id)
		if err != nil {
			return nil, err
		}
		if reset > 0 {
			m.addEvent(ctx, id, 0, "info", "exhausted release searches reset after watch settings changed", mustJSON(map[string]any{"episodes": reset}))
		}
	}
	// Any editable matching, timing, or destination setting can affect what
	// should happen next. Re-evaluate promptly instead of retaining a stale
	// schedule calculated from the previous settings.
	now := m.now()
	if err := m.Store.ScheduleWatchCheck(ctx, id, now); err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

// logSettingsChanged logs one audit event listing only the fields Update
// actually changed. quality_profile is reported as changed without a JSON
// diff: the encoded document is large and not meant for the audit log.
func (m *Manager) logSettingsChanged(ctx context.Context, id int64, before, after Watch) {
	var fields []string
	details := map[string]any{}
	addField := func(name string, changed bool, from, to any) {
		if !changed {
			return
		}
		fields = append(fields, name)
		details[name] = map[string]any{"from": from, "to": to}
	}
	addField("out_dir", before.OutDir != after.OutDir, before.OutDir, after.OutDir)
	addField("organize_by_season", before.OrganizeBySeason != after.OrganizeBySeason, before.OrganizeBySeason, after.OrganizeBySeason)
	addField("search_title", before.SearchTitle != after.SearchTitle, before.SearchTitle, after.SearchTitle)
	addField("fallback_policy", before.FallbackPolicy != after.FallbackPolicy, before.FallbackPolicy, after.FallbackPolicy)
	addField("preferred_wait_seconds", before.PreferredWaitSeconds != after.PreferredWaitSeconds, before.PreferredWaitSeconds, after.PreferredWaitSeconds)
	if before.QualityProfileJSON != after.QualityProfileJSON {
		fields = append(fields, "quality_profile")
		details["quality_profile"] = "changed"
	}
	if len(fields) == 0 {
		return
	}
	m.addEvent(ctx, id, 0, "info", "settings changed: "+strings.Join(fields, ", "), mustJSON(details))
}

func (m *Manager) SetEnabled(ctx context.Context, id int64, enabled bool) (*WatchView, error) {
	w, err := m.Store.GetWatch(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := m.Store.SetWatchEnabled(ctx, id, enabled); err != nil {
		return nil, err
	}
	if enabled {
		now := m.now()
		if err := m.Store.ScheduleWatchCheck(ctx, id, now); err != nil {
			log.Printf("series watch %d: schedule watch check: %v", id, err)
		}
	}
	if enabled != w.Enabled {
		message := "watch paused"
		if enabled {
			message = "watch resumed"
		}
		m.addEvent(ctx, id, 0, "info", message, "")
	}
	return m.Get(ctx, id)
}

func (m *Manager) Remove(ctx context.Context, id int64) error { return m.Store.DeleteWatch(ctx, id) }

// ListAttention returns only durable, server-generated candidate snapshots
// that still require a manual decision.
func (m *Manager) ListAttention(ctx context.Context, watchID int64) ([]AttentionEpisodeView, error) {
	w, err := m.Store.GetWatch(ctx, watchID)
	if err != nil {
		return nil, err
	}
	if w.FallbackPolicy != FallbackManual {
		return []AttentionEpisodeView{}, nil
	}
	episodes, err := m.Store.ListEpisodes(ctx, watchID)
	if err != nil {
		return nil, err
	}
	out := make([]AttentionEpisodeView, 0)
	for _, ep := range episodes {
		if ep.State != StateNeedsAttention || ep.JobID.Valid {
			continue
		}
		candidates, err := attentionCandidates(ep.AttentionCandidatesJSON)
		if err != nil {
			return nil, fmt.Errorf("invalid attention candidate snapshot for episode %d: %w", ep.ID, err)
		}
		out = append(out, AttentionEpisodeView{EpisodeView: episodeView(ep), Candidates: candidates})
	}
	return out, nil
}

// SelectAttentionCandidate accepts only an identifier from the durable
// attention snapshot and queues it with the episode's stable source key.
func (m *Manager) SelectAttentionCandidate(ctx context.Context, watchID, episodeID int64, ident string) (*EpisodeView, error) {
	if !m.startWatchRun(watchID) {
		return nil, ErrWatchCheckInProgress
	}
	defer m.finishWatchRun(watchID)
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return nil, invalid("ident is required")
	}
	w, err := m.Store.GetWatch(ctx, watchID)
	if err != nil {
		return nil, err
	}
	if !w.Enabled {
		return nil, fmt.Errorf("%w: %w", ErrAttentionSelectionConflict, ErrWatchPaused)
	}
	if w.FallbackPolicy != FallbackManual {
		return nil, fmt.Errorf("%w: manual fallback is disabled for this policy", ErrAttentionSelectionConflict)
	}
	ep, err := m.Store.GetEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	if ep.WatchID != w.ID {
		return nil, invalid("episode does not belong to watch")
	}
	if ep.JobID.Valid && ep.ChosenWebshareIdent.Valid && ep.ChosenWebshareIdent.String == ident {
		view := episodeView(*ep)
		return &view, nil
	}
	if !ep.JobID.Valid && ep.ChosenWebshareIdent.Valid && ep.ChosenWebshareIdent.String == ident {
		if err := m.queueSelection(ctx, w, ep, queueReasonManual); err != nil {
			return nil, err
		}
		updated, err := m.Store.GetEpisode(ctx, ep.ID)
		if err != nil {
			return nil, err
		}
		view := episodeView(*updated)
		return &view, nil
	}
	if ep.State != StateNeedsAttention || ep.JobID.Valid {
		return nil, fmt.Errorf("%w: episode is not awaiting manual selection", ErrAttentionSelectionConflict)
	}
	if !resolver.IsValidWebshareIdent(ident) {
		return nil, invalid("invalid candidate identifier")
	}
	candidates, err := attentionCandidates(ep.AttentionCandidatesJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid attention candidate snapshot: %w", err)
	}
	var selected *CandidateView
	for i := range candidates {
		if candidates[i].Ident == ident && candidates[i].Accepted {
			selected = &candidates[i]
			break
		}
	}
	if selected == nil {
		return nil, invalid("candidate is not available for this episode")
	}
	selection := mustJSON(*selected)
	if err := m.Store.SelectAttentionCandidate(ctx, w.ID, ep.ID, selected.Ident, selected.Filename, selection); err != nil {
		return nil, err
	}
	ep.ChosenWebshareIdent = sql.NullString{String: selected.Ident, Valid: true}
	ep.ChosenFilename = sql.NullString{String: selected.Filename, Valid: true}
	if err := m.queueSelection(ctx, w, ep, queueReasonManual); err != nil {
		return nil, err
	}
	updated, err := m.Store.GetEpisode(ctx, ep.ID)
	if err != nil {
		return nil, err
	}
	view := episodeView(*updated)
	return &view, nil
}

// ListEvents returns a watch's check log, newest first, in the job events
// line format "<created_at> <level> <message>".
func (m *Manager) ListEvents(ctx context.Context, id int64, limit int) ([]string, error) {
	if _, err := m.Store.GetWatch(ctx, id); err != nil {
		return nil, err
	}
	events, err := m.Store.ListEvents(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.CreatedAt+" "+e.Level+" "+e.Message)
	}
	return out, nil
}

func (m *Manager) CheckNow(ctx context.Context, id int64) (*WatchView, error) {
	if err := m.processWatchWithRecovery(ctx, id); err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

func (m *Manager) CheckDue(ctx context.Context) error {
	if err := m.syncUnfinishedJobs(ctx); err != nil {
		return err
	}
	watches, err := m.Store.ListDueWatches(ctx, m.now(), 20)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentWatchChecks)
	for _, w := range watches {
		watchID := w.ID
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// processWatch records failures after it acquires the per-watch
			// guard. A concurrent manual run is responsible for its own result.
			_ = m.processWatch(ctx, watchID)
		}()
	}
	wg.Wait()
	return nil
}

// syncUnfinishedJobs mirrors queue job states into episodes between watch
// checks, which may be a day apart. It reads only the local database. A watch
// with a running check is skipped because that check syncs the same episodes
// and would log the same transition again.
func (m *Manager) syncUnfinishedJobs(ctx context.Context) error {
	episodes, err := m.Store.ListUnfinishedJobEpisodes(ctx)
	if err != nil {
		return err
	}
	for start := 0; start < len(episodes); {
		watchID := episodes[start].WatchID
		end := start
		for end < len(episodes) && episodes[end].WatchID == watchID {
			end++
		}
		if m.startWatchRun(watchID) {
			for i := start; i < end; i++ {
				m.syncJobState(ctx, &episodes[i])
			}
			m.finishWatchRun(watchID)
		}
		start = end
	}
	return nil
}

func (m *Manager) processWatch(ctx context.Context, id int64) error {
	return m.processWatchWithOptions(ctx, id, false)
}

// processWatchWithRecovery is used only by the explicit Check now action.
// Manual fallback episodes retain their server-generated candidate snapshot;
// all other exhausted episodes get a new, user-requested search cycle.
func (m *Manager) processWatchWithRecovery(ctx context.Context, id int64) error {
	return m.processWatchWithOptions(ctx, id, true)
}

func (m *Manager) processWatchWithOptions(ctx context.Context, id int64, recoverExhausted bool) error {
	if !m.startWatchRun(id) {
		return ErrWatchCheckInProgress
	}
	defer m.finishWatchRun(id)
	if recoverExhausted {
		w, err := m.Store.GetWatch(ctx, id)
		if err != nil {
			return err
		}
		if !w.Enabled {
			return ErrWatchPaused
		}
		m.addEvent(ctx, id, 0, "info", "manual check requested", "")
		if w.FallbackPolicy != FallbackManual {
			reset, err := m.Store.ResetExhaustedEpisodes(ctx, id)
			if err != nil {
				return err
			}
			if reset > 0 {
				m.addEvent(ctx, id, 0, "info", "exhausted release searches reset by manual check", mustJSON(map[string]any{"episodes": reset}))
			}
		}
	}
	err := m.processWatchUnlocked(ctx, id)
	if err != nil {
		m.recordWatchFailure(ctx, id, err)
	}
	if err := m.Store.PruneEvents(ctx, id, maxEventsPerWatch); err != nil {
		log.Printf("series watch %d: prune events: %v", id, err)
	}
	return err
}

// startWatchRun allows different watches to use external providers in
// parallel, while preventing duplicate work for the same watch. This also
// keeps a scheduler tick and a manual check from racing to queue an episode.
func (m *Manager) startWatchRun(id int64) bool {
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	if m.running == nil {
		m.running = make(map[int64]struct{})
	}
	if _, exists := m.running[id]; exists {
		return false
	}
	m.running[id] = struct{}{}
	return true
}

func (m *Manager) finishWatchRun(id int64) {
	m.runsMu.Lock()
	delete(m.running, id)
	m.runsMu.Unlock()
}

// addEvent records a watch event and logs any store failure instead of
// silently dropping it, since a lost audit entry is otherwise invisible.
func (m *Manager) addEvent(ctx context.Context, watchID, episodeID int64, level, message, details string) {
	if _, err := m.Store.AddEvent(ctx, watchID, episodeID, level, message, details); err != nil {
		log.Printf("series watch %d: add event: %v", watchID, err)
	}
}

func (m *Manager) recordWatchFailure(ctx context.Context, id int64, checkErr error) {
	now := m.now()
	next := now.Add(30 * time.Minute)
	if err := m.Store.UpdateWatchCheck(ctx, id, &now, &next, checkErr.Error()); err != nil {
		log.Printf("series watch %d: update watch check: %v", id, err)
	}
	m.addEvent(ctx, id, 0, "error", "watch check failed", mustJSON(map[string]string{"error": checkErr.Error()}))
}

func (m *Manager) processWatchUnlocked(ctx context.Context, id int64) error {
	w, err := m.Store.GetWatch(ctx, id)
	if err != nil {
		return err
	}
	if !w.Enabled {
		return nil
	}
	now := m.now()
	// Loaded before the upsert loop so a new-vs-tracked episode and an air time
	// change can be told apart from what TVmaze already reported last check.
	before, err := m.Store.ListEpisodes(ctx, id)
	if err != nil {
		return err
	}
	beforeByTVMazeID := make(map[int64]Episode, len(before))
	for _, ep := range before {
		beforeByTVMazeID[ep.TVMazeEpisodeID] = ep
	}
	eps, err := m.TVMaze.Episodes(ctx, w.TVMazeID)
	if err != nil {
		return err
	}
	var newCount int
	var newCodes []string
	for _, remote := range eps {
		if remote.Number == nil || remote.Season <= 0 || *remote.Number <= 0 || !shouldTrack(*w, remote) {
			continue
		}
		existed, tracked := beforeByTVMazeID[remote.ID]
		_, err = m.Store.UpsertEpisode(ctx, EpisodeInput{WatchID: id, TVMazeEpisodeID: remote.ID, Season: remote.Season, Episode: *remote.Number, EpisodeName: remote.Name, AirTimestamp: remote.Airstamp, AirDate: remote.Airdate, AirtimeKnown: remote.Airtime != "", RuntimeMinutes: remote.Runtime, State: StateScheduled})
		if err != nil {
			return err
		}
		if !tracked {
			newCount++
			if len(newCodes) < maxEventCandidates {
				newCodes = append(newCodes, episodeCode(remote.Season, *remote.Number))
			}
			continue
		}
		m.logAirTimeChange(ctx, w, existed, remote)
	}
	if newCount > 0 {
		if len(before) == 0 && !w.LastCheckedAt.Valid {
			// This watch has never completed a check before now: every
			// tracked episode is "new" by definition, so a per-episode diff
			// would just restate the list.
			m.addEvent(ctx, w.ID, 0, "info", fmt.Sprintf("tracking %s from TVmaze", pluralEpisodes(newCount)), "")
		} else {
			list := strings.Join(newCodes, ", ")
			if newCount > len(newCodes) {
				list += ", …"
			}
			m.addEvent(ctx, w.ID, 0, "info", fmt.Sprintf("TVmaze added %s: %s", pluralEpisodes(newCount), list), "")
		}
	}
	stored, err := m.Store.ListEpisodes(ctx, id)
	if err != nil {
		return err
	}
	profile, prefs, err := profileForWatch(w)
	if err != nil {
		return err
	}
	// Date-only episodes need the channel timezone before the loop below.
	showFetched := false
	if !w.AirTimezone.Valid {
		if err := m.refreshShow(ctx, w); err != nil {
			return err
		}
		showFetched = true
	}
	airLoc := airLocation(w.AirTimezone.String)
	var next time.Time
	hasNext := false
	considerNext := func(candidate time.Time) {
		if !hasNext || candidate.Before(next) {
			next = candidate
			hasNext = true
		}
	}
	// handled records whether this check searched or queued anything; a check
	// that did neither still logs one summary line.
	handled := false
	// inFlight records a search or an unfinished job; only a watch with
	// neither and no upcoming episode asks TVmaze whether the show ended.
	inFlight := false
	var nextAiring *Episode
	var nextAiringEnd time.Time
	for i := range stored {
		ep := &stored[i]
		if ep.JobID.Valid {
			m.syncJobState(ctx, ep)
			if !slices.Contains(finalEpisodeStates, ep.State) {
				inFlight = true
			}
			continue
		}
		if ep.ChosenWebshareIdent.Valid {
			if err := m.queueSelection(ctx, w, ep, queueReasonRetry); err != nil {
				return err
			}
			handled = true
			inFlight = true
			continue
		}
		if ep.State == StateNeedsAttention {
			continue
		}
		searchFrom, ok := searchableAt(ep, airLoc)
		if !ok {
			continue
		}
		if now.Before(searchFrom) {
			if err := m.Store.SetEpisodeState(ctx, ep.ID, StateWaitingRelease); err != nil {
				log.Printf("series watch %d: set episode state: %v", w.ID, err)
			}
			considerNext(searchFrom)
			if nextAiring == nil || searchFrom.Before(nextAiringEnd) {
				nextAiring, nextAiringEnd = ep, searchFrom
			}
			continue
		}
		handled = true
		nextSearch, scheduled, err := m.searchRelease(ctx, w, ep, profile, prefs, now)
		if err != nil {
			return err
		}
		// A queued selection leaves the episode in flight like a scheduled
		// search; give-up and review end the search.
		if ep.ChosenWebshareIdent.Valid {
			inFlight = true
		}
		if scheduled {
			considerNext(nextSearch)
			inFlight = true
		}
	}
	idle := nextAiring == nil && !inFlight
	if idle && !showFetched {
		if err := m.refreshShow(ctx, w); err != nil {
			return err
		}
	}
	// Release searches wait for the known episode time. TVmaze metadata is
	// still refreshed daily so schedule changes and newly announced earlier
	// episodes are discovered without resuming the old six-hour polling. An
	// ended show only needs the rare revival or schedule correction noticed.
	refresh := metadataRefreshInterval
	ended := idle && w.ShowStatus == tvmazeShowEnded
	if ended {
		refresh = endedShowCheckInterval
	}
	metadataRefresh := now.Add(refresh)
	if !hasNext || metadataRefresh.Before(next) {
		next = metadataRefresh
	}
	if next.Before(now.Add(minimumCheckInterval)) {
		next = now.Add(minimumCheckInterval)
	}
	if !handled {
		message := noEpisodeDueMessage(nextAiring, nextAiringEnd)
		switch {
		case ended:
			message = "no episode due; series ended, next check " + formatEventTime(next)
		case idle:
			message = "no episode due; waiting for next season"
		}
		m.addEvent(ctx, w.ID, 0, "info", message, "")
	}
	return m.Store.UpdateWatchCheck(ctx, id, &now, &next, "")
}

// searchRelease runs one release search for an aired episode and applies the
// watch's fallback policy. It returns the episode's next search time when the
// episode stays in its search window.
func (m *Manager) searchRelease(ctx context.Context, w *Watch, ep *Episode, profile ReleaseProfile, prefs map[string]string, now time.Time) (time.Time, bool, error) {
	windowStart, err := m.Store.StartEpisodeSearch(ctx, ep.ID, now)
	if err != nil {
		return time.Time{}, false, err
	}
	scored, err := m.searchEpisode(ctx, w.SearchTitle, ep.Season, ep.Episode, profile, prefs)
	if err != nil {
		return time.Time{}, false, err
	}
	code := episodeCode(ep.Season, ep.Episode)
	accepted := acceptedCandidates(scored)
	exactCount := 0
	var chosen *ScoredCandidate
	for j := range accepted {
		if !accepted[j].Exact {
			continue
		}
		exactCount++
		if chosen == nil {
			chosen = &accepted[j]
		}
	}
	reason := queueReasonExact
	preferredWait := time.Duration(w.PreferredWaitSeconds) * time.Second
	waitExpired := !now.Before(windowStart.Add(preferredWait))
	if chosen == nil && w.FallbackPolicy == FallbackBalanced && waitExpired && len(accepted) > 0 {
		chosen = &accepted[0]
		reason = "alternative after " + shortDuration(preferredWait)
	}
	if chosen != nil {
		snapshot := mustJSON(candidateView(*chosen))
		if err := m.Store.SetEpisodeSelection(ctx, ep.ID, chosen.Candidate.Ident, chosen.Candidate.Filename, snapshot); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return time.Time{}, false, nil
			}
			return time.Time{}, false, err
		}
		ep.ChosenWebshareIdent = sql.NullString{String: chosen.Candidate.Ident, Valid: true}
		ep.ChosenFilename = sql.NullString{String: chosen.Candidate.Filename, Valid: true}
		return time.Time{}, false, m.queueSelection(ctx, w, ep, reason)
	}

	attempts, err := m.Store.IncrementEpisodeSearchAttempts(ctx, ep.ID)
	if err != nil {
		return time.Time{}, false, err
	}
	details := mustJSON(map[string]any{"attempt": attempts, "candidates": eventCandidates(scored)})
	if w.FallbackPolicy == FallbackManual && waitExpired && len(accepted) > 0 {
		if err := m.Store.SetEpisodeAttention(ctx, ep.ID, mustJSON(candidateViews(accepted))); err != nil {
			return time.Time{}, false, err
		}
		m.addEvent(ctx, w.ID, ep.ID, "warning", fmt.Sprintf("%s needs review: %d alternatives", code, len(accepted)), details)
		return time.Time{}, false, nil
	}
	if now.Sub(windowStart) >= searchGiveUpAfter {
		if w.FallbackPolicy == FallbackManual {
			if err := m.Store.SetEpisodeAttention(ctx, ep.ID, mustJSON(candidateViews(accepted))); err != nil {
				return time.Time{}, false, err
			}
		} else if err := m.Store.SetEpisodeState(ctx, ep.ID, StateNeedsAttention); err != nil {
			return time.Time{}, false, err
		}
		cause := "no matching release"
		if len(accepted) > 0 {
			cause = fmt.Sprintf("no exact release, %d alternatives", len(accepted))
		}
		m.addEvent(ctx, w.ID, ep.ID, "warning", fmt.Sprintf("%s gave up after %s: %s", code, shortDuration(searchGiveUpAfter), cause), details)
		return time.Time{}, false, nil
	}

	if err := m.Store.SetEpisodeState(ctx, ep.ID, StatePreferredNotFound); err != nil {
		log.Printf("series watch %d: set episode state: %v", w.ID, err)
	}
	next := nextSearchAt(w, windowStart, now)
	if next.Before(now.Add(minimumCheckInterval)) {
		next = now.Add(minimumCheckInterval)
	}
	message := fmt.Sprintf("%s search #%d: %d results, %d accepted, %d exact; next search %s", code, attempts, len(scored), len(accepted), exactCount, formatEventTime(next))
	m.addEvent(ctx, w.ID, ep.ID, "info", message, details)
	return next, true, nil
}

// nextSearchAt applies the search tiers, stops at the give-up time, and moves
// the search forward to the instant the preferred-quality wait expires.
func nextSearchAt(w *Watch, windowStart, now time.Time) time.Time {
	giveUpAt := windowStart.Add(searchGiveUpAfter)
	next := giveUpAt
	age := now.Sub(windowStart)
	for _, tier := range searchTiers {
		if age < tier.before {
			next = now.Add(tier.every)
			break
		}
	}
	if next.After(giveUpAt) {
		next = giveUpAt
	}
	if w.FallbackPolicy == FallbackStrict {
		return next
	}
	fallbackAt := windowStart.Add(time.Duration(w.PreferredWaitSeconds) * time.Second)
	if fallbackAt.After(now) && fallbackAt.Before(next) {
		next = fallbackAt
	}
	return next
}

// refreshShow stores the TVmaze show status and channel timezone on w.
func (m *Manager) refreshShow(ctx context.Context, w *Watch) error {
	show, err := m.TVMaze.Show(ctx, w.TVMazeID)
	if err != nil {
		return err
	}
	airTimezone := show.airTimezone()
	oldStatus, oldTimezone := w.ShowStatus, w.AirTimezone
	if err := m.Store.SetWatchShow(ctx, w.ID, show.Status, airTimezone); err != nil {
		return err
	}
	w.ShowStatus = show.Status
	w.AirTimezone = sql.NullString{String: airTimezone, Valid: true}
	// "" means never fetched before; only a change from a known prior value is
	// worth an event.
	if oldStatus != "" && oldStatus != show.Status {
		m.addEvent(ctx, w.ID, 0, "info", fmt.Sprintf("show status %s → %s", oldStatus, show.Status), "")
	}
	if oldTimezone.Valid && oldTimezone.String != "" && oldTimezone.String != airTimezone {
		m.addEvent(ctx, w.ID, 0, "info", fmt.Sprintf("air timezone %s → %s", oldTimezone.String, airTimezone), "")
	}
	return nil
}

// airLocation falls back to UTC when the timezone is unknown or tzdata lacks
// it.
func airLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// searchableAt is when an episode's release can first exist. Without an
// airtime (streaming drops) TVmaze's airstamp is a placeholder that can be
// later than the real drop, so the air date's local midnight is used instead.
func searchableAt(ep *Episode, airLoc *time.Location) (time.Time, bool) {
	if !ep.AirtimeKnown && ep.AirDate.Valid {
		day, err := time.ParseInLocation(tvmazeAirDateLayout, ep.AirDate.String, airLoc)
		if err == nil {
			return day, true
		}
	}
	air, ok := parseNullTime(ep.AirTimestamp)
	if !ok {
		return time.Time{}, false
	}
	return air.Add(episodeRuntime(ep)), true
}

func episodeRuntime(ep *Episode) time.Duration {
	if !ep.RuntimeMinutes.Valid || ep.RuntimeMinutes.Int64 <= 0 {
		return defaultEpisodeRuntime
	}
	return time.Duration(ep.RuntimeMinutes.Int64) * time.Minute
}

// logAirTimeChange notes a TVmaze schedule correction for an episode already
// tracked. A final episode (completed/failed/skipped/decrypt_failed) no
// longer acts on its schedule, so a correction there is not worth an event.
func (m *Manager) logAirTimeChange(ctx context.Context, w *Watch, existed Episode, remote TVMazeEpisode) {
	if slices.Contains(finalEpisodeStates, existed.State) {
		return
	}
	var newTimestamp sql.NullString
	if remote.Airstamp != nil {
		newTimestamp = sql.NullString{String: remote.Airstamp.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	// Matches the store's nullOrValue: a whitespace-only date is not a value.
	newDate := sql.NullString{String: remote.Airdate, Valid: strings.TrimSpace(remote.Airdate) != ""}
	if existed.AirTimestamp == newTimestamp && existed.AirDate == newDate {
		return
	}
	code := episodeCode(remote.Season, *remote.Number)
	message := fmt.Sprintf("%s air time changed: %s → %s", code, airTimeLabel(existed.AirTimestamp, existed.AirDate), airTimeLabel(newTimestamp, newDate))
	m.addEvent(ctx, w.ID, existed.ID, "info", message, "")
}

// airTimeLabel renders a known air time for an event message, falling back to
// the raw air date when no timestamp parses.
func airTimeLabel(timestamp, airDate sql.NullString) string {
	if t, ok := parseNullTime(timestamp); ok {
		return formatEventTime(t)
	}
	if airDate.Valid {
		return airDate.String
	}
	return "unknown"
}

func noEpisodeDueMessage(next *Episode, airEnd time.Time) string {
	if next == nil {
		return "no episode due; no upcoming episode known"
	}
	return fmt.Sprintf("no episode due; next %s ends %s", episodeLabel(next), formatEventTime(airEnd))
}

func episodeCode(season, episode int) string {
	return fmt.Sprintf("S%02dE%02d", season, episode)
}

// pluralEpisodes renders an episode count for an event message, e.g.
// "1 episode" or "3 episodes".
func pluralEpisodes(n int) string {
	if n == 1 {
		return "1 episode"
	}
	return fmt.Sprintf("%d episodes", n)
}

func episodeLabel(ep *Episode) string {
	code := episodeCode(ep.Season, ep.Episode)
	if strings.TrimSpace(ep.EpisodeName) == "" {
		return code
	}
	return code + ` "` + ep.EpisodeName + `"`
}

func formatEventTime(t time.Time) string {
	return t.UTC().Format(eventTimeLayout)
}

func shortDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	}
	return d.String()
}

// eventCandidates keeps event details small; scored is already ordered
// accepted first, then by score.
func eventCandidates(scored []ScoredCandidate) []CandidateView {
	if len(scored) > maxEventCandidates {
		scored = scored[:maxEventCandidates]
	}
	return candidateViews(scored)
}

// queueSelection creates the download job for a durable selection. reason is
// shown in the check log, e.g. "exact" or "alternative after 24h".
func (m *Manager) queueSelection(ctx context.Context, w *Watch, ep *Episode, reason string) error {
	if m.Queue == nil {
		return errors.New("queue service not configured")
	}
	outDir, err := m.episodeOutDir(w, ep.Season)
	if err != nil {
		return err
	}
	url := "https://webshare.cz/#/file/" + ep.ChosenWebshareIdent.String
	sourceKey := fmt.Sprintf("series:%d:episode:%d", w.ID, ep.TVMazeEpisodeID)
	var jobID int64
	if keyed, ok := m.Queue.(SourceKeyQueueCreator); ok {
		jobID, err = keyed.CreateJobWithSourceKey(ctx, url, outDir, "", "webshare", "", 0, sourceKey)
	} else {
		jobID, err = m.Queue.CreateJob(ctx, url, outDir, "", "webshare", "", 0)
	}
	if err != nil {
		return err
	}
	if err := m.Store.AttachJob(ctx, ep.ID, jobID, StateQueued); err != nil {
		return err
	}
	message := fmt.Sprintf("%s queued job #%d (%s): %s", episodeCode(ep.Season, ep.Episode), jobID, reason, ep.ChosenFilename.String)
	m.addEvent(ctx, w.ID, ep.ID, "info", message, mustJSON(map[string]any{"job_id": jobID, "webshare_ident": ep.ChosenWebshareIdent.String, "filename": ep.ChosenFilename.String, "out_dir": outDir}))
	return nil
}

func (m *Manager) searchEpisode(ctx context.Context, title string, season, episode int, profile ReleaseProfile, prefs map[string]string) ([]ScoredCandidate, error) {
	if m.Webshare == nil {
		return nil, errors.New("Webshare client not configured")
	}
	code := episodeCode(season, episode)
	queries := []string{strings.TrimSpace(title + " " + code), strings.ReplaceAll(strings.TrimSpace(title), " ", ".") + "." + code}
	seen := map[string]bool{}
	var candidates []ReleaseCandidate
	for _, q := range queries {
		results, err := m.Webshare.SearchVideos(ctx, q, 50, 1)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			if seen[r.Ident] {
				continue
			}
			seen[r.Ident] = true
			candidates = append(candidates, ReleaseCandidate{Ident: r.Ident, Filename: r.Filename, Size: r.Size, Category: r.Category, Available: r.Available, Password: r.Password, Removed: r.Removed, Encrypted: r.Encrypted, Rating: r.Rating})
		}
	}
	return EvaluateCandidates(profile, candidates, MatchOptions{Season: season, Episode: episode, ExpectedTitle: title, Preferences: prefs}), nil
}

func (m *Manager) reference(ctx context.Context, req CreateRequest) (string, string, ReleaseProfile, error) {
	ident := strings.TrimSpace(req.ReferenceWebshareIdent)
	raw := firstNonEmpty(req.ReferenceURL, req.ReferenceWebshareURL)
	if ident == "" {
		ident = resolver.ExtractWebshareIdent(raw)
	}
	if !resolver.IsValidWebshareIdent(ident) {
		return "", "", ReleaseProfile{}, invalid("invalid Webshare reference URL")
	}
	filename := strings.TrimSpace(req.ReferenceFilename)
	if filename == "" {
		if m.Webshare == nil {
			return "", "", ReleaseProfile{}, errors.New("Webshare client not configured")
		}
		info, err := m.Webshare.FileInfo(ctx, ident)
		if err != nil {
			return "", "", ReleaseProfile{}, err
		}
		filename = info.Filename
	}
	p := ParseReleaseName(filename)
	return ident, filename, p, nil
}

func (m *Manager) view(ctx context.Context, w *Watch) (*WatchView, error) {
	eps, err := m.Store.ListEpisodes(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	now := m.now()
	v := &WatchView{ID: w.ID, Enabled: w.Enabled, TVMazeID: w.TVMazeID, DisplayName: w.DisplayName, SearchTitle: w.SearchTitle, ReferenceWebshareIdent: w.ReferenceWebshareIdent, ReferenceFilename: w.ReferenceFilename, OutDir: w.OutDir, SeriesFolder: w.SeriesFolder, OrganizeBySeason: w.OrganizeBySeason, QualityProfile: json.RawMessage(w.QualityProfileJSON), FallbackPolicy: w.FallbackPolicy, PreferredWaitSeconds: w.PreferredWaitSeconds, NextCheckAt: nullString(w.NextCheckAt), LastCheckedAt: nullString(w.LastCheckedAt), LastError: nullString(w.LastError), ShowStatus: w.ShowStatus, Status: "active", CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
	if !w.Enabled {
		v.Status = "paused"
	}
	airLoc := airLocation(w.AirTimezone.String)
	for i := range eps {
		ev := episodeView(eps[i])
		if eps[i].State == StateNeedsAttention {
			v.AttentionCount++
		}
		air, ok := parseNullTime(eps[i].AirTimestamp)
		if !ok {
			continue
		}
		// A date-only episode is already searched before its placeholder
		// airstamp, so it must leave "next" as soon as it is searchable.
		searchable, _ := searchableAt(&eps[i], airLoc)
		if searchable.After(now) {
			if v.NextEpisode == nil || air.Before(mustParseTime(v.NextEpisode.AirTimestamp)) {
				copy := ev
				v.NextEpisode = &copy
				v.NextSearchAt = searchable.UTC().Format(time.RFC3339)
			}
		} else {
			if v.LastEpisode == nil || air.After(mustParseTime(v.LastEpisode.AirTimestamp)) {
				copy := ev
				v.LastEpisode = &copy
			}
		}
	}
	if v.AttentionCount > 0 && w.Enabled {
		v.Status = StateNeedsAttention
	}
	return v, nil
}

func (m *Manager) syncJobState(ctx context.Context, ep *Episode) {
	if m.JobState == nil {
		return
	}
	job, err := m.JobState(ctx, ep.JobID.Int64)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.skipRemovedJob(ctx, ep)
			return
		}
		log.Printf("series watch %d: job state: %v", ep.WatchID, err)
		return
	}
	if job.Status == queue.StatusDeleted {
		m.skipRemovedJob(ctx, ep)
		return
	}
	state := job.Status
	mapped := state
	switch state {
	case "resolving":
		mapped = StateQueued
	case "paused":
		mapped = StateQueued
	case "decrypting", "decrypt_failed":
		mapped = state
	}
	if mapped == ep.State {
		return
	}
	if err := m.Store.SetEpisodeState(ctx, ep.ID, mapped); err != nil {
		log.Printf("series watch %d: set episode state: %v", ep.WatchID, err)
		return
	}
	ep.State = mapped
	level := "info"
	message := fmt.Sprintf("%s job #%d %s", episodeCode(ep.Season, ep.Episode), ep.JobID.Int64, mapped)
	if mapped == StateFailed || mapped == queue.StatusDecryptFail {
		level = "error"
		if job.Error != "" {
			message += ": " + job.Error
		}
	}
	m.addEvent(ctx, ep.WatchID, ep.ID, level, message, "")
}

// skipRemovedJob moves an episode whose job was soft- or hard-deleted from
// the queue to StateSkipped instead of searching again: the jobs source_key
// unique index keeps the soft-deleted row, so re-queueing the episode would
// re-attach the deleted job. A user requeue of that job is still mirrored back
// by the daily check. Final episodes (skipped included) are left alone, since
// clearing completed jobs must not rewrite finished downloads.
func (m *Manager) skipRemovedJob(ctx context.Context, ep *Episode) {
	if slices.Contains(finalEpisodeStates, ep.State) {
		return
	}
	if err := m.Store.SetEpisodeState(ctx, ep.ID, StateSkipped); err != nil {
		log.Printf("series watch %d: set episode state: %v", ep.WatchID, err)
		return
	}
	ep.State = StateSkipped
	message := fmt.Sprintf("%s job #%d removed from queue; episode skipped", episodeCode(ep.Season, ep.Episode), ep.JobID.Int64)
	m.addEvent(ctx, ep.WatchID, ep.ID, "warning", message, "")
}

func decodeProfile(raw json.RawMessage, fallback ReleaseProfile) (ReleaseProfile, map[string]string, error) {
	p := fallback
	prefs := map[string]string{"resolution": PreferenceRequire, "codec": PreferencePrefer, "source": PreferencePrefer, "release_group": PreferencePrefer, "container": PreferenceIgnore}
	if len(raw) == 0 || string(raw) == "null" {
		return p, prefs, nil
	}
	var envelope storedProfile
	if json.Unmarshal(raw, &envelope) == nil && envelope.Profile.Filename != "" {
		if envelope.Profile.SearchTitle == "" {
			envelope.Profile.SearchTitle = fallback.SearchTitle
		}
		if len(envelope.Profile.Episodes) == 0 {
			envelope.Profile.Episodes = fallback.Episodes
		}
		for k, v := range envelope.Preferences {
			prefs[k] = normalizeMode(v)
		}
		return envelope.Profile, prefs, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return p, nil, invalid("invalid quality_profile")
	}
	for key, value := range fields {
		var item struct {
			Value      any    `json:"value"`
			Normalized any    `json:"normalized"`
			Mode       string `json:"mode"`
		}
		var text string
		if json.Unmarshal(value, &item) == nil && (item.Value != nil || item.Normalized != nil || item.Mode != "") {
			chosen := item.Normalized
			if chosen == nil {
				chosen = item.Value
			}
			text = fmt.Sprint(chosen)
			if item.Mode != "" {
				prefs[key] = normalizeMode(item.Mode)
			}
		} else {
			_ = json.Unmarshal(value, &text)
		}
		setProfileField(&p, key, text)
	}
	return p, prefs, nil
}

func profileForWatch(w *Watch) (ReleaseProfile, map[string]string, error) {
	var stored storedProfile
	if err := json.Unmarshal([]byte(w.QualityProfileJSON), &stored); err == nil && stored.Profile.SearchTitle != "" {
		return stored.Profile, stored.Preferences, nil
	}
	p := ParseReleaseName(w.ReferenceFilename)
	return decodeProfile(json.RawMessage(w.QualityProfileJSON), p)
}
func setProfileField(p *ReleaseProfile, key, value string) {
	switch key {
	case "resolution":
		p.Resolution = value
	case "codec":
		p.Codec = value
	case "source":
		p.Source = value
	case "release_group":
		p.ReleaseGroup = canonicalGroup(value)
	case "container":
		p.Container = strings.ToLower(value)
	case "hdr":
		p.HDR = value
	}
}
func normalizeMode(v string) string {
	switch strings.ToLower(v) {
	case "required", "require":
		return PreferenceRequire
	case "ignored", "ignore":
		return PreferenceIgnore
	default:
		return PreferencePrefer
	}
}
func (m *Manager) cleanOutDir(path string) (string, error) {
	return queue.CleanOutDir(strings.TrimSpace(path), m.AllowedRoots)
}

func (m *Manager) validateOutDir(path string) error {
	_, err := m.cleanOutDir(path)
	return err
}

func (m *Manager) episodeOutDir(w *Watch, season int) (string, error) {
	if w == nil || season <= 0 {
		return "", errors.New("valid watch and season are required")
	}
	root, err := m.cleanOutDir(w.OutDir)
	if err != nil {
		return "", err
	}
	if !w.OrganizeBySeason {
		return root, nil
	}
	// Users often pick an existing season folder as out_dir; appending another
	// season folder would nest them (".../s04/s04").
	if seasonFolderRe.MatchString(filepath.Base(root)) {
		if parent, err := m.cleanOutDir(filepath.Dir(root)); err == nil {
			root = parent
		}
	}
	return m.cleanOutDir(filepath.Join(root, fmt.Sprintf("s%02d", season)))
}

func normalizeSeriesFolder(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("series_folder must contain letters or numbers")
	}
	if filepath.IsAbs(input) || strings.ContainsAny(input, "/\\") || input == "." || input == ".." {
		return "", errors.New("series_folder must be a single folder name")
	}
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(input) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			dash = false
			continue
		}
		if out.Len() > 0 && !dash {
			out.WriteByte('-')
			dash = true
		}
	}
	folder := strings.Trim(out.String(), "-")
	if folder == "" || folder == "." || folder == ".." {
		return "", errors.New("series_folder must contain letters or numbers")
	}
	return folder, nil
}
func validPolicy(v string) bool {
	return v == FallbackStrict || v == FallbackBalanced || v == FallbackManual
}
func validStartMode(v string) bool {
	return v == StartModeTemplate || v == StartModeContinue || v == StartModeSpecific
}
func shouldTrack(w Watch, e TVMazeEpisode) bool {
	switch w.StartMode {
	case StartModeContinue:
		if !w.StartSeason.Valid || !w.StartEpisode.Valid {
			return true
		}
		return e.Season > int(w.StartSeason.Int64) || (e.Season == int(w.StartSeason.Int64) && e.Number != nil && *e.Number > int(w.StartEpisode.Int64))
	case StartModeSpecific:
		if !w.StartSeason.Valid || !w.StartEpisode.Valid {
			return false
		}
		return e.Season > int(w.StartSeason.Int64) || (e.Season == int(w.StartSeason.Int64) && e.Number != nil && *e.Number >= int(w.StartEpisode.Int64))
	default:
		created, err := time.Parse(time.RFC3339Nano, w.CreatedAt)
		return err != nil || e.Airstamp == nil || e.Airstamp.After(created)
	}
}
func choosePreviewEpisode(eps []TVMazeEpisode, p ReleaseProfile, r CreateRequest, now time.Time) *TVMazeEpisode {
	sort.SliceStable(eps, func(i, j int) bool {
		if eps[i].Season != eps[j].Season {
			return eps[i].Season < eps[j].Season
		}
		if eps[i].Number == nil {
			return false
		}
		if eps[j].Number == nil {
			return true
		}
		return *eps[i].Number < *eps[j].Number
	})
	mode := firstNonEmpty(r.StartMode, r.InitialMode, StartModeTemplate)
	for i := range eps {
		e := &eps[i]
		if e.Number == nil {
			continue
		}
		if mode == StartModeSpecific && (e.Season < r.InitialSeason || (e.Season == r.InitialSeason && *e.Number < r.InitialEpisode)) {
			continue
		}
		if mode == StartModeContinue && len(p.Episodes) > 0 {
			last := p.Episodes[len(p.Episodes)-1]
			if e.Season < last.Season || (e.Season == last.Season && *e.Number <= last.Episode) {
				continue
			}
		}
		if mode == StartModeTemplate && e.Airstamp != nil && e.Airstamp.Before(now) {
			continue
		}
		return e
	}
	return nil
}
func acceptedCandidates(in []ScoredCandidate) []ScoredCandidate {
	out := make([]ScoredCandidate, 0, len(in))
	for _, c := range in {
		if c.Accepted {
			out = append(out, c)
		}
	}
	return out
}
func candidateView(c ScoredCandidate) CandidateView {
	return CandidateView{Ident: c.Candidate.Ident, Filename: c.Candidate.Filename, SizeBytes: c.Candidate.Size, Score: c.Score, Accepted: c.Accepted, Exact: c.Exact, Reasons: c.Reasons, RejectReasons: c.RejectedReasons, Profile: c.Profile}
}

func hasRejectReason(c ScoredCandidate, reason string) bool {
	for _, rejected := range c.RejectedReasons {
		if rejected == reason {
			return true
		}
	}
	return false
}

func candidateViews(in []ScoredCandidate) []CandidateView {
	out := make([]CandidateView, 0, len(in))
	for _, c := range in {
		out = append(out, candidateView(c))
	}
	return out
}

func attentionCandidates(snapshot sql.NullString) ([]CandidateView, error) {
	if !snapshot.Valid || strings.TrimSpace(snapshot.String) == "" {
		return []CandidateView{}, nil
	}
	var candidates []CandidateView
	if err := json.Unmarshal([]byte(snapshot.String), &candidates); err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if !candidate.Accepted || !resolver.IsValidWebshareIdent(candidate.Ident) || strings.TrimSpace(candidate.Filename) == "" {
			return nil, errors.New("invalid candidate snapshot")
		}
	}
	return candidates, nil
}

func episodeView(e Episode) EpisodeView {
	v := EpisodeView{ID: e.ID, TVMazeEpisodeID: e.TVMazeEpisodeID, Season: e.Season, Episode: e.Episode, EpisodeName: e.EpisodeName, AirTimestamp: nullString(e.AirTimestamp), State: e.State, SearchAttempts: e.SearchAttempts, UpdatedAt: e.UpdatedAt}
	if e.ChosenFilename.Valid {
		v.ChosenFilename = e.ChosenFilename.String
	}
	if e.JobID.Valid {
		v.JobID = e.JobID.Int64
	}
	return v
}
func parseNullTime(v sql.NullString) (time.Time, bool) {
	if !v.Valid {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		t, err = time.Parse(time.RFC3339, v.String)
	}
	return t, err == nil
}
func mustParseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	if t.IsZero() {
		t, _ = time.Parse(time.RFC3339, v)
	}
	return t
}
func nullString(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid series id")
	}
	return id, nil
}
