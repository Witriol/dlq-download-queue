package queue

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	downloadclient "github.com/Witriol/dlq-download-queue/internal/downloader"
	"github.com/Witriol/dlq-download-queue/internal/resolver"
)

type Runner struct {
	Store              *Store
	Resolvers          *resolver.Registry
	Downloader         Downloader
	ArchiveDecryptor   ArchiveDecryptor
	Concurrency        int        // static fallback
	GetConcurrency     func() int // dynamic getter (preferred if set)
	GetAutoDecrypt     func() bool
	DecryptConcurrency int // decrypt worker concurrency (default 1)
	PollEvery          time.Duration

	decryptMu      sync.Mutex
	decryptPending map[int64]struct{}
	decryptSem     chan struct{}
}

type decryptTask struct {
	jobID       int64
	archivePath string
	outDir      string
	password    string
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
	if err := r.dispatchCompletedDecrypt(ctx); err != nil {
		log.Printf("runner dispatchCompletedDecrypt error: %v", err)
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
			if errors.Is(err, downloadclient.ErrGIDNotFound) {
				_ = r.Store.MarkFailed(ctx, job.ID, "gid_not_found", err.Error(), time.Now().UTC().Add(2*time.Minute))
				continue
			}
			_ = r.Store.AddEvent(ctx, job.ID, "error", err.Error())
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
			_ = r.Store.AddEvent(ctx, job.ID, "info", "download finished")
			if r.queueDecryptFromStatus(ctx, job, st, bytesDone) {
				continue
			}
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

func (r *Runner) queueDecryptFromStatus(ctx context.Context, job Job, st *downloadclient.Status, bytesDone int64) bool {
	if !r.autoDecryptEnabled() {
		return false
	}
	if r.ArchiveDecryptor == nil {
		return false
	}
	password := strings.TrimSpace(nullString(job.ArchivePassword))
	archivePath := firstStatusPath(st)
	if archivePath == "" {
		archivePath = archivePathForJob(job)
	}
	if archivePath == "" {
		msg := "archive decrypt failed: missing archive path"
		_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
		_ = r.Store.MarkDecryptFailed(ctx, job.ID, msg)
		_ = r.Store.ClearArchivePassword(ctx, job.ID)
		return true
	}
	if !isArchiveFile(archivePath) {
		_ = r.Store.AddEvent(ctx, job.ID, "info", "archive decrypt skipped: not an archive")
		_ = r.Store.ClearArchivePassword(ctx, job.ID)
		return false
	}
	if err := r.Store.MarkDecrypting(ctx, job.ID, bytesDone); err != nil {
		log.Printf("runner mark decrypting error for job %d: %v", job.ID, err)
		return false
	}
	_ = r.Store.AddEvent(ctx, job.ID, "info", "archive decrypt started: "+filepath.Base(archivePath))
	r.scheduleDecrypt(ctx, decryptTask{
		jobID:       job.ID,
		archivePath: archivePath,
		outDir:      job.OutDir,
		password:    password,
	})
	return true
}

func (r *Runner) dispatchCompletedDecrypt(ctx context.Context) error {
	if !r.autoDecryptEnabled() || r.ArchiveDecryptor == nil {
		return nil
	}
	jobs, err := r.Store.ListPendingArchiveDecrypt(ctx, 100)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		password := strings.TrimSpace(nullString(job.ArchivePassword))
		archivePath := archivePathForJob(job)
		if archivePath == "" || !isArchiveFile(archivePath) {
			msg := "archive decrypt failed: missing archive path"
			if archivePath != "" && !isArchiveFile(archivePath) {
				msg = "archive decrypt failed: not an archive"
			}
			_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
			_ = r.Store.MarkDecryptFailed(ctx, job.ID, msg)
			_ = r.Store.ClearArchivePassword(ctx, job.ID)
			continue
		}
		if job.Status != StatusDecrypting {
			if err := r.Store.MarkDecrypting(ctx, job.ID, job.BytesDone); err != nil {
				log.Printf("runner mark decrypting error for job %d: %v", job.ID, err)
				continue
			}
			_ = r.Store.AddEvent(ctx, job.ID, "info", "archive decrypt started: "+filepath.Base(archivePath))
		}
		r.scheduleDecrypt(ctx, decryptTask{
			jobID:       job.ID,
			archivePath: archivePath,
			outDir:      job.OutDir,
			password:    password,
		})
	}
	return nil
}

func (r *Runner) scheduleDecrypt(ctx context.Context, task decryptTask) {
	if !r.markDecryptPending(task.jobID) {
		return
	}
	go r.runDecrypt(ctx, task)
}

func (r *Runner) runDecrypt(ctx context.Context, task decryptTask) {
	if !r.acquireDecryptWorker(ctx) {
		r.unmarkDecryptPending(task.jobID)
		return
	}
	defer r.releaseDecryptWorker()
	defer r.unmarkDecryptPending(task.jobID)

	attempted, err := r.ArchiveDecryptor.MaybeDecrypt(ctx, task.archivePath, task.outDir, task.password)
	if err != nil {
		eventMsg := "archive decrypt failed: " + err.Error()
		_ = r.Store.AddEvent(ctx, task.jobID, "error", eventMsg)
		if markErr := r.Store.MarkDecryptFailed(ctx, task.jobID, "archive decrypt failed"); markErr != nil {
			log.Printf("runner mark decrypt failed error for job %d: %v", task.jobID, markErr)
		}
	} else if attempted {
		_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypted: "+filepath.Base(task.archivePath))
		if markErr := r.Store.MarkCompleted(ctx, task.jobID); markErr != nil {
			log.Printf("runner mark completed error for job %d: %v", task.jobID, markErr)
		}
	} else {
		_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypt skipped: not an archive")
		if markErr := r.Store.MarkCompleted(ctx, task.jobID); markErr != nil {
			log.Printf("runner mark completed error for job %d: %v", task.jobID, markErr)
		}
	}
	if err := r.Store.ClearArchivePassword(ctx, task.jobID); err != nil {
		log.Printf("runner clear archive password error for job %d: %v", task.jobID, err)
	}
}

func (r *Runner) autoDecryptEnabled() bool {
	if r.GetAutoDecrypt != nil {
		return r.GetAutoDecrypt()
	}
	return false
}

func (r *Runner) decryptConcurrency() int {
	if r.DecryptConcurrency > 0 {
		return r.DecryptConcurrency
	}
	return 1
}

func (r *Runner) decryptSemaphore() chan struct{} {
	r.decryptMu.Lock()
	defer r.decryptMu.Unlock()
	if r.decryptSem != nil {
		return r.decryptSem
	}
	sem := make(chan struct{}, r.decryptConcurrency())
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
	r.decryptSem = sem
	return r.decryptSem
}

func (r *Runner) acquireDecryptWorker(ctx context.Context) bool {
	sem := r.decryptSemaphore()
	select {
	case <-ctx.Done():
		return false
	case <-sem:
		return true
	}
}

func (r *Runner) releaseDecryptWorker() {
	sem := r.decryptSemaphore()
	sem <- struct{}{}
}

func (r *Runner) markDecryptPending(jobID int64) bool {
	r.decryptMu.Lock()
	defer r.decryptMu.Unlock()
	if r.decryptPending == nil {
		r.decryptPending = make(map[int64]struct{})
	}
	if _, exists := r.decryptPending[jobID]; exists {
		return false
	}
	r.decryptPending[jobID] = struct{}{}
	return true
}

func (r *Runner) unmarkDecryptPending(jobID int64) {
	r.decryptMu.Lock()
	defer r.decryptMu.Unlock()
	delete(r.decryptPending, jobID)
}

func firstStatusPath(st *downloadclient.Status) string {
	if st == nil {
		return ""
	}
	for _, f := range st.Files {
		p := strings.TrimSpace(f.Path)
		if p != "" {
			return p
		}
	}
	return ""
}

func archivePathForJob(job Job) string {
	name := sanitizeFilename(job.Name)
	if name == "" && job.Filename.Valid {
		name = sanitizeFilename(job.Filename.String)
	}
	if name == "" {
		return ""
	}
	return filepath.Join(job.OutDir, name)
}

func nullString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
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
