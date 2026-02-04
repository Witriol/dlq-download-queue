package queue

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Witriol/my-downloader/internal/resolver"
)

type Runner struct {
	Store          *Store
	Resolvers      *resolver.Registry
	Downloader     Downloader
	Concurrency    int            // static fallback
	GetConcurrency func() int     // dynamic getter (preferred if set)
	PollEvery      time.Duration
}

func (r *Runner) concurrency() int {
	if r.GetConcurrency != nil {
		if c := r.GetConcurrency(); c > 0 {
			return c
		}
	}
	if r.Concurrency > 0 {
		return r.Concurrency
	}
	return 2
}

func (r *Runner) Start(ctx context.Context) {
	if r.PollEvery <= 0 {
		r.PollEvery = 2 * time.Second
	}
	ticker := time.NewTicker(r.PollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	// Update downloading jobs first.
	if err := r.updateActive(ctx); err != nil {
		log.Printf("runner updateActive error: %v", err)
	}
	if err := r.requeueFailed(ctx); err != nil {
		log.Printf("runner requeueFailed error: %v", err)
	}
	// Start new jobs if capacity.
	active := r.countDownloading(ctx)
	for active < r.concurrency() {
		job, err := r.Store.ClaimNextQueued(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			log.Printf("claim job error: %v", err)
			break
		}
		if err := r.resolveAndStart(ctx, job); err != nil {
			log.Printf("job %d resolve/start error: %v", job.ID, err)
		}
		active = r.countDownloading(ctx)
	}
}

func (r *Runner) resolveAndStart(ctx context.Context, job *Job) error {
	latest, err := r.Store.GetJob(ctx, job.ID)
	if err == nil && latest.DeletedAt.Valid {
		return r.Store.AddEvent(ctx, job.ID, "info", "skipped deleted job")
	}
	res, err := r.Resolvers.ResolveWithSite(ctx, job.Site, job.URL)
	if err != nil {
		code, msg, retryAt := mapResolverError(err)
		_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
		return r.Store.MarkFailed(ctx, job.ID, code, msg, retryAt)
	}
	filename := sanitizeFilename(res.Filename)
	if err := r.Store.UpdateResolving(ctx, job.ID, res.URL, filename, res.Size); err != nil {
		return err
	}
	if res.Kind != "aria2" {
		code := "unsupported_engine"
		msg := "resolver returned unsupported engine"
		_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
		return r.Store.MarkFailed(ctx, job.ID, code, msg, time.Now().UTC().Add(30*time.Minute))
	}
	options := map[string]string{
		"dir": job.OutDir,
	}
	if name := sanitizeFilename(job.Name); name != "" {
		options["out"] = name
	} else if filename != "" {
		options["out"] = filename
	}
	for k, v := range res.Options {
		if v == "" {
			continue
		}
		options[k] = v
	}
	if len(res.Headers) > 0 {
		var b strings.Builder
		first := true
		for k, v := range res.Headers {
			if !first {
				b.WriteString("\n")
			}
			first = false
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
		}
		options["header"] = b.String()
	}
	gid, err := r.Downloader.AddURI(ctx, res.URL, options)
	if err != nil {
		_ = r.Store.AddEvent(ctx, job.ID, "error", err.Error())
		return r.Store.MarkFailed(ctx, job.ID, "download_start_failed", err.Error(), time.Now().UTC().Add(10*time.Minute))
	}
	_ = r.Store.AddEvent(ctx, job.ID, "info", "download started")
	return r.Store.MarkDownloading(ctx, job.ID, "aria2", gid)
}

func (r *Runner) requeueFailed(ctx context.Context) error {
	ids, err := r.Store.ListRetryableFailed(ctx, 0)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.Store.Requeue(ctx, id); err != nil {
			_ = r.Store.AddEvent(ctx, id, "error", "auto requeue failed: "+err.Error())
			continue
		}
		_ = r.Store.AddEvent(ctx, id, "info", "auto retry queued")
	}
	return nil
}

func (r *Runner) updateActive(ctx context.Context) error {
	jobs, err := r.Store.ListJobs(ctx, StatusDownloading, false)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !job.EngineGID.Valid {
			continue
		}
		st, err := r.Downloader.TellStatus(ctx, job.EngineGID.String)
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "not found") || strings.Contains(msg, "status") {
				_ = r.Store.MarkFailed(ctx, job.ID, "gid_not_found", msg, time.Now().UTC().Add(2*time.Minute))
				continue
			}
			_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
			continue
		}
		bytesDone, _ := strconv.ParseInt(st.CompletedLen, 10, 64)
		totalLen, _ := strconv.ParseInt(st.TotalLength, 10, 64)
		speed, _ := strconv.ParseInt(st.DownloadSpeed, 10, 64)
		eta := int64(0)
		if speed > 0 && totalLen > 0 && bytesDone < totalLen {
			eta = (totalLen - bytesDone) / speed
		}
		switch st.Status {
		case "complete":
			if bytesDone == 0 && totalLen > 0 {
				bytesDone = totalLen
			}
			_ = r.Store.UpdateProgress(ctx, job.ID, bytesDone, StatusCompleted, 0, 0)
			_ = r.Store.MarkCompleted(ctx, job.ID)
		case "error":
			msg := st.ErrorMessage
			if msg == "" {
				msg = "download error"
			}
			_ = r.Store.MarkFailed(ctx, job.ID, "download_error", msg, time.Now().UTC().Add(10*time.Minute))
		default:
			_ = r.Store.UpdateProgress(ctx, job.ID, bytesDone, StatusDownloading, speed, eta)
		}
	}
	return nil
}

func (r *Runner) countDownloading(ctx context.Context) int {
	jobs, err := r.Store.ListJobs(ctx, StatusDownloading, false)
	if err != nil {
		return 0
	}
	return len(jobs)
}

func mapResolverError(err error) (code, msg string, retryAt time.Time) {
	switch {
	case errors.Is(err, resolver.ErrLoginRequired):
		return "login_required", "login required or file not public", time.Now().UTC().Add(6 * time.Hour)
	case errors.Is(err, resolver.ErrQuotaExceeded):
		return "quota_exceeded", "quota exceeded; retry later", time.Now().UTC().Add(2 * time.Hour)
	case errors.Is(err, resolver.ErrCaptchaNeeded):
		return "captcha_needed", "captcha required; cannot proceed in headless mode", time.Now().UTC().Add(24 * time.Hour)
	case errors.Is(err, resolver.ErrTemporarilyOff):
		return "temporarily_unavailable", "temporarily unavailable; retry later", time.Now().UTC().Add(30 * time.Minute)
	case errors.Is(err, resolver.ErrUnknownSite):
		return "unknown_site", "unknown site; cannot resolve", time.Now().UTC().Add(6 * time.Hour)
	default:
		return "resolve_failed", err.Error(), time.Now().UTC().Add(30 * time.Minute)
	}
}
