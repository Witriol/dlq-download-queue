// Package series contains the durable state and external metadata primitives
// used by the series watcher. It intentionally does not know about the queue
// service; a scheduler can use the JobID field to connect the two domains.
package series

import (
	"database/sql"
	"errors"
	"time"
)

const (
	StateScheduled         = "scheduled"
	StateWaitingRelease    = "waiting_release"
	StateSearching         = "searching"
	StatePreferredNotFound = "preferred_not_found"
	StateNeedsAttention    = "needs_attention"
	StateQueued            = "queued"
	StateDownloading       = "downloading"
	StateCompleted         = "completed"
	StateFailed            = "failed"
	StateSkipped           = "skipped"
)

const (
	FallbackStrict    = "strict"
	FallbackBalanced  = "balanced"
	FallbackManual    = "manual"
	StartModeTemplate = "template"
	StartModeContinue = "continue"
	StartModeSpecific = "specific"
	StartModeFuture   = StartModeTemplate
)

const DefaultPreferredWait = 24 * time.Hour

// ValidationError marks a request as invalid without relying on error text at
// the HTTP boundary. The wrapped error remains available for diagnostics.
type ValidationError struct{ err error }

func (e *ValidationError) Error() string { return e.err.Error() }
func (e *ValidationError) Unwrap() error { return e.err }

func invalid(message string) error { return &ValidationError{err: errors.New(message)} }

func invalidWrap(err error) error {
	if err == nil {
		return nil
	}
	return &ValidationError{err: err}
}

// Watch is a persisted long-lived series subscription. Nullable timestamps
// and start coordinates are represented with sql.Null* values because the
// database stores timestamps as RFC3339 text and a watch need not have run yet.
type Watch struct {
	ID                     int64
	Enabled                bool
	TVMazeID               int64
	DisplayName            string
	SearchTitle            string
	ReferenceWebshareIdent string
	ReferenceFilename      string
	OutDir                 string
	SeriesFolder           string
	OrganizeBySeason       bool
	QualityProfileJSON     string
	StartMode              string
	StartSeason            sql.NullInt64
	StartEpisode           sql.NullInt64
	FallbackPolicy         string
	PreferredWaitSeconds   int64
	NextCheckAt            sql.NullString
	LastCheckedAt          sql.NullString
	LastError              sql.NullString
	CreatedAt              string
	UpdatedAt              string
}

// Episode is one TVmaze episode tracked by a watch. TVmaze specials (which
// have no season/episode number) are normally filtered before persistence.
type Episode struct {
	ID                      int64
	WatchID                 int64
	TVMazeEpisodeID         int64
	Season                  int
	Episode                 int
	EpisodeName             string
	AirTimestamp            sql.NullString
	State                   string
	ChosenWebshareIdent     sql.NullString
	ChosenFilename          sql.NullString
	SelectionSnapshotJSON   sql.NullString
	AttentionCandidatesJSON sql.NullString
	SearchAttempts          int
	JobID                   sql.NullInt64
	RuntimeMinutes          sql.NullInt64
	SearchStartedAt         sql.NullString
	CreatedAt               string
	UpdatedAt               string
}

type WatchEvent struct {
	ID          int64
	WatchID     int64
	EpisodeID   sql.NullInt64
	Level       string
	Message     string
	DetailsJSON sql.NullString
	CreatedAt   string
}

// EpisodeInput contains the TVmaze fields needed to upsert an episode. An
// absent AirTimestamp is valid and leaves the episode eligible for a slower
// manual/periodic check.
type EpisodeInput struct {
	WatchID         int64
	TVMazeEpisodeID int64
	Season          int
	Episode         int
	EpisodeName     string
	AirTimestamp    *time.Time
	RuntimeMinutes  *int
	State           string
}

// TVMazeShow is the stable subset of a TVmaze show retained by callers when
// asking a user to explicitly select a show.
type TVMazeShow struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type,omitempty"`
	Language  string   `json:"language,omitempty"`
	Genres    []string `json:"genres,omitempty"`
	Status    string   `json:"status,omitempty"`
	Premiered string   `json:"premiered,omitempty"`
	Ended     string   `json:"ended,omitempty"`
	URL       string   `json:"url,omitempty"`
}

type TVMazeSearchResult struct {
	Score float64    `json:"score"`
	Show  TVMazeShow `json:"show"`
}

// TVMazeEpisode uses pointers for fields that the TVmaze API returns as null
// for specials or incomplete schedule data.
type TVMazeEpisode struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Season   int        `json:"season"`
	Number   *int       `json:"number"`
	Airdate  string     `json:"airdate"`
	Airtime  string     `json:"airtime"`
	Airstamp *time.Time `json:"airstamp"`
	Runtime  *int       `json:"runtime"`
	Type     string     `json:"type,omitempty"`
	Summary  string     `json:"summary,omitempty"`
}
