package queue

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
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
	MegaDecryptor      MegaDecryptor
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
	megaPath    string
	archivePath string
	outDir      string
	password    string
	rawURL      string
	site        string
	decryptMega bool
	decryptArch bool
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
	if outName := sanitizeFilename(options["out"]); outName != "" {
		if err := r.prepareOutputForStart(ctx, job, outName, options); err != nil {
			_ = r.Store.AddEvent(ctx, job.ID, "error", "prepare output failed: "+err.Error())
			return r.Store.MarkFailed(ctx, job.ID, "prepare_output_failed", err.Error(), time.Now().UTC().Add(10*time.Minute))
		}
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

func (r *Runner) prepareOutputForStart(ctx context.Context, job *Job, outName string, options map[string]string) error {
	if !needsFreshStart(options) {
		return nil
	}
	if r.hasActiveOutputConflict(ctx, job, outName) {
		return nil
	}
	controlPath := filepath.Join(job.OutDir, outName) + ".aria2"
	if err := os.Remove(controlPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func needsFreshStart(options map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(options["continue"]), "false") ||
		strings.EqualFold(strings.TrimSpace(options["always-resume"]), "false")
}

func (r *Runner) hasActiveOutputConflict(ctx context.Context, job *Job, outName string) bool {
	if r.Store == nil || job == nil || outName == "" {
		return false
	}
	jobs, err := r.Store.ListJobs(ctx, "", false)
	if err != nil {
		log.Printf("runner output conflict check error for job %d: %v", job.ID, err)
		return false
	}
	for _, other := range jobs {
		if other.ID == job.ID {
			continue
		}
		if other.OutDir != job.OutDir {
			continue
		}
		if !isOutputConflictActiveStatus(other.Status) {
			continue
		}
		if outputNameForJob(other) == outName {
			return true
		}
	}
	return false
}

func isOutputConflictActiveStatus(status string) bool {
	switch status {
	case StatusQueued, StatusResolving, StatusDownloading, StatusPaused, StatusDecrypting:
		return true
	default:
		return false
	}
}

func outputNameForJob(job Job) string {
	if name := sanitizeFilename(job.Name); name != "" {
		return name
	}
	if job.Filename.Valid {
		return sanitizeFilename(job.Filename.String)
	}
	return ""
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
			if r.queueDecryptFromStatus(ctx, job, st, bytesDone) {
				continue
			}
			_ = r.Store.UpdateProgress(ctx, job.ID, bytesDone, StatusCompleted, 0, 0)
			_ = r.Store.AddEvent(ctx, job.ID, "info", "download finished")
			_ = r.Store.MarkCompleted(ctx, job.ID)
		case "error":
			msg := st.ErrorMessage
			if msg == "" {
				msg = "download error"
			}
			code, retryAt := mapDownloadError(msg)
			_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
			_ = r.Store.MarkFailed(ctx, job.ID, code, msg, retryAt)
		default:
			_ = r.Store.UpdateProgress(ctx, job.ID, bytesDone, StatusDownloading, speed, eta)
		}
	}
	return nil
}

func (r *Runner) queueDecryptFromStatus(ctx context.Context, job Job, st *downloadclient.Status, bytesDone int64) bool {
	task, shouldProcess, waitMsg, failMsg := r.buildDecryptTask(ctx, job, st)
	if !shouldProcess {
		return false
	}
	if failMsg != "" {
		_ = r.Store.AddEvent(ctx, job.ID, "error", failMsg)
		_ = r.Store.MarkPostprocessFailed(ctx, job.ID, failMsg, "postprocess_failed")
		_ = r.Store.ClearArchivePassword(ctx, job.ID)
		return true
	}
	if err := r.Store.MarkDecrypting(ctx, job.ID, bytesDone); err != nil {
		log.Printf("runner mark decrypting error for job %d: %v", job.ID, err)
		return false
	}
	_ = r.Store.AddEvent(ctx, job.ID, "info", "download finished")
	if waitMsg != "" {
		_ = r.Store.AddEvent(ctx, job.ID, "info", waitMsg)
		return true
	}
	r.scheduleDecrypt(ctx, task)
	return true
}

func (r *Runner) dispatchCompletedDecrypt(ctx context.Context) error {
	if r.ArchiveDecryptor == nil && r.MegaDecryptor == nil {
		return nil
	}
	jobs, err := r.Store.ListPendingPostprocess(ctx, 100)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !r.autoDecryptEnabled() && job.Status != StatusDecrypting && !r.shouldDecryptMega(job, archivePathForJob(job)) {
			continue
		}
		task, shouldProcess, waitMsg, failMsg := r.buildDecryptTask(ctx, job, nil)
		if !shouldProcess {
			if job.Status == StatusDecrypting {
				if markErr := r.Store.MarkCompleted(ctx, job.ID); markErr != nil {
					log.Printf("runner mark completed error for job %d: %v", job.ID, markErr)
				}
				_ = r.Store.ClearArchivePassword(ctx, job.ID)
			}
			continue
		}
		if failMsg != "" {
			_ = r.Store.AddEvent(ctx, job.ID, "error", failMsg)
			_ = r.Store.MarkPostprocessFailed(ctx, job.ID, failMsg, "postprocess_failed")
			_ = r.Store.ClearArchivePassword(ctx, job.ID)
			continue
		}
		markedDecrypting := false
		if job.Status != StatusDecrypting {
			if err := r.Store.MarkDecrypting(ctx, job.ID, job.BytesDone); err != nil {
				log.Printf("runner mark decrypting error for job %d: %v", job.ID, err)
				continue
			}
			markedDecrypting = true
		}
		if waitMsg != "" {
			if markedDecrypting {
				_ = r.Store.AddEvent(ctx, job.ID, "info", waitMsg)
			}
			continue
		}
		r.scheduleDecrypt(ctx, task)
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

	if task.decryptMega && r.MegaDecryptor != nil {
		_ = r.Store.AddEvent(ctx, task.jobID, "info", "mega decrypt started: "+filepath.Base(task.megaPath))
		attempted, err := r.MegaDecryptor.MaybeDecrypt(ctx, task.site, task.rawURL, task.megaPath)
		if err != nil {
			eventMsg := "mega decrypt failed: " + err.Error()
			_ = r.Store.AddEvent(ctx, task.jobID, "error", eventMsg)
			if markErr := r.Store.MarkPostprocessFailed(ctx, task.jobID, "mega decrypt failed", "mega_decrypt_failed"); markErr != nil {
				log.Printf("runner mark mega decrypt failed error for job %d: %v", task.jobID, markErr)
			}
			_ = r.Store.ClearArchivePassword(ctx, task.jobID)
			return
		}
		if attempted {
			_ = r.Store.AddEvent(ctx, task.jobID, "info", "mega decrypted: "+filepath.Base(task.megaPath))
		}
	}
	if task.decryptArch && r.ArchiveDecryptor != nil {
		_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypt started: "+filepath.Base(task.archivePath))
		attempted, err := r.ArchiveDecryptor.MaybeDecrypt(ctx, task.archivePath, task.outDir, task.password)
		if err != nil {
			eventMsg := "archive decrypt failed: " + err.Error()
			_ = r.Store.AddEvent(ctx, task.jobID, "error", eventMsg)
			if markErr := r.Store.MarkPostprocessFailed(ctx, task.jobID, "archive decrypt failed", "archive_decrypt_failed"); markErr != nil {
				log.Printf("runner mark archive decrypt failed error for job %d: %v", task.jobID, markErr)
			}
			_ = r.Store.ClearArchivePassword(ctx, task.jobID)
			return
		}
		if attempted {
			_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypted: "+filepath.Base(task.archivePath))
		} else {
			_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypt skipped: not an archive")
		}
	}
	if markErr := r.Store.MarkCompleted(ctx, task.jobID); markErr != nil {
		log.Printf("runner mark completed error for job %d: %v", task.jobID, markErr)
	}
	if err := r.Store.ClearArchivePassword(ctx, task.jobID); err != nil {
		log.Printf("runner clear archive password error for job %d: %v", task.jobID, err)
	}
}

func (r *Runner) buildDecryptTask(ctx context.Context, job Job, st *downloadclient.Status) (decryptTask, bool, string, string) {
	password := strings.TrimSpace(nullString(job.ArchivePassword))
	downloadPath := firstStatusPath(st)
	if downloadPath == "" {
		downloadPath = archivePathForJob(job)
	}
	archivePath := resolveArchiveEntryPath(downloadPath)
	task := decryptTask{
		jobID:       job.ID,
		megaPath:    downloadPath,
		archivePath: archivePath,
		outDir:      job.OutDir,
		password:    password,
		rawURL:      job.URL,
		site:        job.Site,
		decryptMega: r.shouldDecryptMega(job, downloadPath),
		decryptArch: r.shouldDecryptArchive(job, archivePath, password),
	}
	if !task.decryptMega && !task.decryptArch {
		return task, false, "", ""
	}
	if task.decryptMega && strings.TrimSpace(task.megaPath) == "" {
		return task, true, "", "postprocess failed: missing file path for mega decrypt"
	}
	if task.decryptArch && strings.TrimSpace(task.archivePath) == "" {
		return task, true, "", "postprocess failed: missing file path for archive decrypt"
	}
	if task.decryptArch {
		if waitMsg := r.archiveDecryptWaitMessage(ctx, job, task.archivePath); waitMsg != "" {
			return task, true, waitMsg, ""
		}
	}
	return task, true, "", ""
}

func (r *Runner) archiveDecryptWaitMessage(ctx context.Context, job Job, filePath string) string {
	if r == nil || r.Store == nil {
		return ""
	}
	groupKey, groupExplicit := multipartArchiveGroupKey(filePath)
	if groupKey == "" {
		return ""
	}
	jobs, err := r.Store.ListJobs(ctx, "", false)
	if err != nil {
		log.Printf("runner multipart wait scan error for job %d: %v", job.ID, err)
		return ""
	}
	pendingParts := 0
	seenSibling := false
	for _, other := range jobs {
		if other.ID == job.ID {
			continue
		}
		if other.OutDir != job.OutDir {
			continue
		}
		otherPath := archivePathForJob(other)
		otherGroupKey, otherExplicit := multipartArchiveGroupKey(otherPath)
		if otherGroupKey == "" || otherGroupKey != groupKey {
			continue
		}
		seenSibling = true
		if otherExplicit {
			groupExplicit = true
		}
		if isMultipartSiblingPending(other.Status) {
			pendingParts++
		}
	}
	if !groupExplicit && !seenSibling {
		return ""
	}
	if pendingParts > 0 {
		return "archive decrypt waiting: multipart set still downloading (" + strconv.Itoa(pendingParts) + " part job(s))"
	}
	return ""
}

func isMultipartSiblingPending(status string) bool {
	switch status {
	case StatusQueued, StatusResolving, StatusDownloading, StatusPaused:
		return true
	default:
		return false
	}
}

func (r *Runner) shouldDecryptMega(job Job, filePath string) bool {
	if r.MegaDecryptor == nil {
		return false
	}
	if !IsMegaJob(job.Site, job.URL) {
		return false
	}
	// Retrying archive-only postprocess failures should not re-run MEGA payload decrypt.
	// The file was already decrypted earlier and re-running can trigger MAC mismatch.
	if job.Status == StatusDecrypting && isArchiveFile(filePath) {
		code := strings.TrimSpace(nullString(job.ErrorCode))
		if code != "" {
			return false
		}
	}
	return true
}

func (r *Runner) shouldDecryptArchive(job Job, filePath, password string) bool {
	if r.ArchiveDecryptor == nil {
		return false
	}
	// Jobs already in decrypting state were queued earlier and should finish regardless of
	// current auto-decrypt setting.
	if job.Status == StatusDecrypting {
		return strings.TrimSpace(password) != "" || isArchiveFile(filePath)
	}
	if !r.autoDecryptEnabled() {
		return false
	}
	return strings.TrimSpace(password) != "" || isArchiveFile(filePath)
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

func mapDownloadError(msg string) (code string, retryAt time.Time) {
	now := time.Now().UTC()
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.Contains(lower, "status=509"),
		strings.Contains(lower, "status code 509"),
		strings.Contains(lower, "status 509"):
		return "quota_exceeded", now.Add(2 * time.Hour)
	default:
		return "download_error", now.Add(10 * time.Minute)
	}
}
