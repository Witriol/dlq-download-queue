package queue

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	downloadclient "github.com/Witriol/my-downloader/internal/downloader"
)

var (
	ErrDownloaderNotConfigured = errors.New("downloader_not_configured")
	ErrMissingEngineGID        = errors.New("missing_engine_gid")
	ErrActionNotAllowed        = errors.New("action_not_allowed")
)

type Service struct {
	store        *Store
	downloader   Downloader
	allowedRoots []string
}

func NewService(store *Store, dl Downloader, allowedRoots []string) *Service {
	roots := make([]string, 0, len(allowedRoots))
	roots = append(roots, allowedRoots...)
	return &Service{store: store, downloader: dl, allowedRoots: roots}
}

func (s *Service) CreateJob(ctx context.Context, url, outDir, name, site string, maxAttempts int) (int64, error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	cleanOut, err := cleanOutDir(outDir, s.allowedRoots)
	if err != nil {
		return 0, err
	}
	cleanName, err := cleanUserFilename(name)
	if err != nil {
		return 0, err
	}
	job := &Job{URL: url, OutDir: cleanOut, Name: cleanName, Site: site, MaxAttempts: maxAttempts}
	id, err := s.store.CreateJob(ctx, job)
	if err != nil {
		return 0, err
	}
	msg := "added url=" + url + " out=" + cleanOut
	if cleanName != "" {
		msg += " name=" + cleanName
	}
	if site != "" {
		msg += " site=" + site
	}
	msg += " max_attempts=" + strconv.Itoa(maxAttempts)
	_ = s.store.AddEvent(ctx, id, "info", msg)
	return id, nil
}

func (s *Service) ListJobs(ctx context.Context, status string, includeDeleted bool) ([]JobView, error) {
	jobs, err := s.store.ListJobs(ctx, status, includeDeleted)
	if err != nil {
		return nil, err
	}
	out := make([]JobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toView(j))
	}
	return out, nil
}

func (s *Service) GetJob(ctx context.Context, id int64) (*JobView, error) {
	j, err := s.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	v := toView(*j)
	return &v, nil
}

func (s *Service) ListEvents(ctx context.Context, id int64, limit int) ([]string, error) {
	return s.store.ListEvents(ctx, id, limit)
}

func (s *Service) Retry(ctx context.Context, id int64) error {
	job, err := s.store.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if job.EngineGID.Valid && s.downloader != nil {
		_ = s.downloader.Remove(ctx, job.EngineGID.String)
	}
	if err := s.store.Requeue(ctx, id); err != nil {
		return err
	}
	return s.store.AddEvent(ctx, id, "info", "retried")
}

func (s *Service) Remove(ctx context.Context, id int64) error {
	job, err := s.store.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if job.EngineGID.Valid && s.downloader != nil {
		_ = s.downloader.Remove(ctx, job.EngineGID.String)
	}
	if err := s.store.Remove(ctx, id); err != nil {
		return err
	}
	return s.store.AddEvent(ctx, id, "info", "removed")
}

func (s *Service) Clear(ctx context.Context) error {
	return s.store.ClearCompleted(ctx)
}

func (s *Service) Purge(ctx context.Context) error {
	return s.store.ClearAll(ctx)
}

func (s *Service) Pause(ctx context.Context, id int64) error {
	if s.downloader == nil {
		return ErrDownloaderNotConfigured
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if !job.EngineGID.Valid {
		return ErrMissingEngineGID
	}
	if err := s.downloader.Pause(ctx, job.EngineGID.String); err != nil {
		if errors.Is(err, downloadclient.ErrActionNotAllowed) {
			return fmt.Errorf("%w: %v", ErrActionNotAllowed, err)
		}
		return err
	}
	if err := s.store.MarkPaused(ctx, id); err != nil {
		return err
	}
	return s.store.AddEvent(ctx, id, "info", "paused")
}

func (s *Service) Resume(ctx context.Context, id int64) error {
	if s.downloader == nil {
		return ErrDownloaderNotConfigured
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if !job.EngineGID.Valid {
		if err := s.store.Requeue(ctx, id); err != nil {
			return err
		}
		return s.store.AddEvent(ctx, id, "info", "resume requeued")
	}
	if err := s.downloader.Unpause(ctx, job.EngineGID.String); err != nil {
		if errors.Is(err, downloadclient.ErrGIDNotFound) {
			if err := s.store.Requeue(ctx, id); err != nil {
				return err
			}
			return s.store.AddEvent(ctx, id, "info", "resume requeued")
		}
		if errors.Is(err, downloadclient.ErrActionNotAllowed) {
			return fmt.Errorf("%w: %v", ErrActionNotAllowed, err)
		}
		return err
	}
	if err := s.store.MarkDownloadingStatus(ctx, id); err != nil {
		return err
	}
	return s.store.AddEvent(ctx, id, "info", "resumed")
}

// JobView is a light view for API/CLI.
type JobView struct {
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	Site          string `json:"site"`
	OutDir        string `json:"out_dir"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Filename      string `json:"filename,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	BytesDone     int64  `json:"bytes_done"`
	DownloadSpeed int64  `json:"download_speed"`
	EtaSeconds    int64  `json:"eta_seconds"`
	Error         string `json:"error,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func toView(j Job) JobView {
	v := JobView{
		ID:        j.ID,
		URL:       j.URL,
		Site:      j.Site,
		OutDir:    j.OutDir,
		Name:      j.Name,
		Status:    j.Status,
		BytesDone: j.BytesDone,
		CreatedAt: j.CreatedAt,
		UpdatedAt: j.UpdatedAt,
	}
	if j.Filename.Valid {
		v.Filename = j.Filename.String
	}
	if j.SizeBytes.Valid {
		v.SizeBytes = j.SizeBytes.Int64
	}
	if j.Error.Valid {
		v.Error = j.Error.String
	}
	if j.ErrorCode.Valid {
		v.ErrorCode = j.ErrorCode.String
	}
	if j.DownloadSpeed.Valid {
		v.DownloadSpeed = j.DownloadSpeed.Int64
	}
	if j.EtaSeconds.Valid {
		v.EtaSeconds = j.EtaSeconds.Int64
	}
	return v
}

var _ interface {
	CreateJob(context.Context, string, string, string, string, int) (int64, error)
	ListJobs(context.Context, string, bool) ([]JobView, error)
	GetJob(context.Context, int64) (*JobView, error)
	ListEvents(context.Context, int64, int) ([]string, error)
	Retry(context.Context, int64) error
	Remove(context.Context, int64) error
	Clear(context.Context) error
	Purge(context.Context) error
	Pause(context.Context, int64) error
	Resume(context.Context, int64) error
} = (*Service)(nil)
