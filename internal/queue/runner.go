package queue

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	downloadclient "github.com/Witriol/dlq-download-queue/internal/downloader"
	"github.com/Witriol/dlq-download-queue/internal/notify"
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
	Notify             func(notify.Item) // nil = no-op

	decryptMu      sync.Mutex
	decryptPending map[int64]struct{}
	decryptSem     chan struct{}

	// completeMu serializes MarkCompleted with the multipart-group
	// completion gate below, so concurrent decrypt workers finishing
	// sibling parts cannot both observe the group as complete.
	completeMu sync.Mutex
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
		return r.markFailed(ctx, job.ID, code, msg, retryAt)
	}
	filename := sanitizeFilename(res.Filename)
	if err := r.Store.UpdateResolving(ctx, job.ID, res.URL, filename, res.Size); err != nil {
		return err
	}
	if res.Kind != "aria2" {
		code := "unsupported_engine"
		msg := "resolver returned unsupported engine"
		_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
		return r.markFailed(ctx, job.ID, code, msg, time.Now().UTC().Add(30*time.Minute))
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
			return r.markFailed(ctx, job.ID, "prepare_output_failed", err.Error(), time.Now().UTC().Add(10*time.Minute))
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
		return r.markFailed(ctx, job.ID, "download_start_failed", err.Error(), time.Now().UTC().Add(10*time.Minute))
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
				_ = r.markFailed(ctx, job.ID, "gid_not_found", err.Error(), time.Now().UTC().Add(2*time.Minute))
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
			// Record the final bytesDone without flipping status to completed here:
			// markCompleted does that under completeMu, so a sibling decrypt worker
			// checking the multipart group's completion never observes this job as
			// completed before markCompleted itself runs.
			_ = r.Store.UpdateProgress(ctx, job.ID, bytesDone, StatusDownloading, 0, 0)
			_ = r.Store.AddEvent(ctx, job.ID, "info", "download finished")
			_ = r.markCompleted(ctx, job.ID)
		case "error":
			msg := st.ErrorMessage
			if msg == "" {
				msg = "download error"
			}
			code, retryAt := mapDownloadError(msg)
			_ = r.Store.AddEvent(ctx, job.ID, "error", msg)
			_ = r.markFailed(ctx, job.ID, code, msg, retryAt)
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
		_ = r.markPostprocessFailed(ctx, job.ID, failMsg, "postprocess_failed")
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
				// Same ordering as runDecrypt: clear the password before
				// markCompleted so a later poll cannot see it as still-completed-
				// with-password and re-dispatch decrypt.
				_ = r.Store.ClearArchivePassword(ctx, job.ID)
				if markErr := r.markCompleted(ctx, job.ID); markErr != nil {
					log.Printf("runner mark completed error for job %d: %v", job.ID, markErr)
				}
			}
			continue
		}
		if failMsg != "" {
			_ = r.Store.AddEvent(ctx, job.ID, "error", failMsg)
			_ = r.markPostprocessFailed(ctx, job.ID, failMsg, "postprocess_failed")
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
			if markErr := r.markPostprocessFailed(ctx, task.jobID, "mega decrypt failed", "mega_decrypt_failed"); markErr != nil {
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
		_, groupExplicit := multipartArchiveGroupKey(task.archivePath)
		siblingDone := groupExplicit && func() bool {
			_, err := os.Stat(task.archivePath)
			return errors.Is(err, os.ErrNotExist)
		}()
		if siblingDone {
			_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypt skipped: already extracted by sibling job")
		} else {
			_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypt started: "+filepath.Base(task.archivePath))
			attempted, err := r.ArchiveDecryptor.MaybeDecrypt(ctx, task.archivePath, task.outDir, task.password)
			if err != nil {
				_ = r.Store.AddEvent(ctx, task.jobID, "error", "archive decrypt failed: "+err.Error())
				if markErr := r.markPostprocessFailed(ctx, task.jobID, err.Error(), "archive_decrypt_failed"); markErr != nil {
					log.Printf("runner mark archive decrypt failed error for job %d: %v", task.jobID, markErr)
				}
				_ = r.Store.ClearArchivePassword(ctx, task.jobID)
				return
			}
			if attempted {
				_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypted: "+filepath.Base(task.archivePath))
				r.deleteArchiveFiles(ctx, task)
			} else {
				_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive decrypt skipped: not an archive")
			}
		}
	}
	// Clear the password before markCompleted (and its Notify): otherwise
	// ListPendingPostprocess can pick this job up again as still-completed-
	// with-password and dispatchCompletedDecrypt re-runs decrypt on it.
	if err := r.Store.ClearArchivePassword(ctx, task.jobID); err != nil {
		log.Printf("runner clear archive password error for job %d: %v", task.jobID, err)
	}
	if markErr := r.markCompleted(ctx, task.jobID); markErr != nil {
		log.Printf("runner mark completed error for job %d: %v", task.jobID, markErr)
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
	groupKey, groupExplicit, siblings, err := r.multipartSiblings(ctx, job, filePath)
	if groupKey == "" {
		return ""
	}
	if err != nil {
		log.Printf("runner multipart wait scan error for job %d: %v", job.ID, err)
		return ""
	}
	if !groupExplicit && len(siblings) == 0 {
		return ""
	}
	latestSiblings := latestArchiveJobsByPart(siblings)
	pendingParts := 0
	for _, sibling := range latestSiblings {
		if isMultipartSiblingBlocking(sibling) {
			pendingParts++
		}
	}
	if pendingParts > 0 {
		return "archive decrypt waiting: multipart set still downloading (" + strconv.Itoa(pendingParts) + " part job(s))"
	}
	return ""
}

// multipartSiblings scans for jobs sharing filePath's multipart archive
// group key. groupExplicit is true if filePath's own key is explicit or
// any sibling's is; siblings excludes job itself. This is the sibling
// scan shared by archiveDecryptWaitMessage and the completed-notify gate.
func (r *Runner) multipartSiblings(ctx context.Context, job Job, filePath string) (groupKey string, groupExplicit bool, siblings []Job, err error) {
	groupKey, groupExplicit = multipartArchiveGroupKey(filePath)
	if groupKey == "" {
		return "", false, nil, nil
	}
	jobs, err := r.Store.ListJobs(ctx, "", false)
	if err != nil {
		return groupKey, groupExplicit, nil, err
	}
	siblings = make([]Job, 0)
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
		siblings = append(siblings, other)
		if otherExplicit {
			groupExplicit = true
		}
	}
	return groupKey, groupExplicit, siblings, nil
}

// multipartGroupKeyFor applies the archiveDecryptWaitMessage detection
// rule (group key non-empty and explicit, or has siblings) to decide
// whether job belongs to a multipart group for notification purposes.
func (r *Runner) multipartGroupKeyFor(ctx context.Context, job Job) (groupKey string, siblings []Job, err error) {
	key, explicit, sib, scanErr := r.multipartSiblings(ctx, job, archivePathForJob(job))
	if scanErr != nil {
		return "", nil, scanErr
	}
	if key == "" || (!explicit && len(sib) == 0) {
		return "", nil, nil
	}
	return key, sib, nil
}

// markCompleted wraps Store.MarkCompleted and, if Notify is set, emits a
// completed notification. The whole operation runs under completeMu so a
// multipart group's last-finishing part reliably observes its siblings.
func (r *Runner) markCompleted(ctx context.Context, jobID int64) error {
	r.completeMu.Lock()
	defer r.completeMu.Unlock()

	if err := r.Store.MarkCompleted(ctx, jobID); err != nil {
		return err
	}
	if r.Notify == nil {
		return nil
	}

	job, err := r.Store.GetJob(ctx, jobID)
	if err != nil {
		log.Printf("runner notify: get job %d error: %v", jobID, err)
		return nil
	}

	r.notifyCompleted(ctx, *job)
	return nil
}

// notifyCompleted enqueues the completed event. A job in a multipart
// archive group only fires once every latest sibling part is also
// completed, and the item then describes the whole group.
func (r *Runner) notifyCompleted(ctx context.Context, job Job) {
	groupKey, siblings, err := r.multipartGroupKeyFor(ctx, job)
	if err != nil {
		log.Printf("runner notify multipart scan error for job %d: %v", job.ID, err)
		return
	}
	if groupKey == "" {
		r.Notify(jobNotifyItem(job, notify.EventCompleted))
		return
	}

	all := append(append([]Job{}, siblings...), job)
	latest := latestArchiveJobsByPart(all)
	for _, part := range latest {
		if part.Status != StatusCompleted {
			return // not every part has completed yet
		}
	}
	r.Notify(groupCompletedItem(job, groupKey, latest))
}

// markFailed wraps Store.MarkFailed and, if Notify is set, emits a
// failed or retrying notification depending on whether attempts are
// exhausted.
func (r *Runner) markFailed(ctx context.Context, jobID int64, code, msg string, nextRetry time.Time) error {
	if err := r.Store.MarkFailed(ctx, jobID, code, msg, nextRetry); err != nil {
		return err
	}
	if r.Notify == nil {
		return nil
	}

	job, err := r.Store.GetJob(ctx, jobID)
	if err != nil {
		log.Printf("runner notify: get job %d error: %v", jobID, err)
		return nil
	}

	event := notify.EventRetrying
	if job.MaxAttempts > 0 && job.Attempts >= job.MaxAttempts {
		event = notify.EventFailed
	}
	r.notifyJobEvent(ctx, *job, event)
	return nil
}

// markPostprocessFailed wraps Store.MarkPostprocessFailed and, if Notify
// is set, emits an extract_failed notification.
func (r *Runner) markPostprocessFailed(ctx context.Context, jobID int64, msg, code string) error {
	if err := r.Store.MarkPostprocessFailed(ctx, jobID, msg, code); err != nil {
		return err
	}
	if r.Notify == nil {
		return nil
	}

	job, err := r.Store.GetJob(ctx, jobID)
	if err != nil {
		log.Printf("runner notify: get job %d error: %v", jobID, err)
		return nil
	}

	r.notifyJobEvent(ctx, *job, notify.EventExtractFailed)
	return nil
}

// notifyJobEvent enqueues a single-job item, tagging it with its
// multipart group key (if any) so the notify batcher can collapse
// same-window failures across parts.
func (r *Runner) notifyJobEvent(ctx context.Context, job Job, event notify.Event) {
	groupKey, _, err := r.multipartGroupKeyFor(ctx, job)
	if err != nil {
		log.Printf("runner notify multipart scan error for job %d: %v", job.ID, err)
	}
	item := jobNotifyItem(job, event)
	item.GroupKey = groupKey
	r.Notify(item)
}

// jobNotifyItem builds a single-job notify.Item from job's fresh fields.
func jobNotifyItem(job Job, event notify.Event) notify.Item {
	return notify.Item{
		Event:       event,
		JobIDs:      []int64{job.ID},
		Name:        job.Name,
		Filename:    nullString(job.Filename),
		Site:        job.Site,
		URL:         job.URL,
		Dir:         job.OutDir,
		Status:      job.Status,
		Error:       nullString(job.Error),
		ErrorCode:   nullString(job.ErrorCode),
		SizeBytes:   jobSizeBytes(job),
		Parts:       1,
		Attempts:    job.Attempts,
		MaxAttempts: job.MaxAttempts,
		SourceKey:   nullString(job.SourceKey),
		StartedAt:   parseJobTime(job.StartedAt),
		FinishedAt:  jobFinishedAt(job, event),
	}
}

// groupCompletedItem describes a completed multipart group: sizes
// summed, ids joined, earliest start, latest finish across parts. job is
// the part whose completion triggered the gate; its own fields (name,
// site, url, dir, ...) seed the item.
func groupCompletedItem(job Job, groupKey string, parts []Job) notify.Item {
	item := jobNotifyItem(job, notify.EventCompleted)
	item.GroupKey = groupKey
	item.Parts = len(parts)

	ids := make([]int64, 0, len(parts))
	var size int64
	var start, finish time.Time
	for _, p := range parts {
		ids = append(ids, p.ID)
		size += jobSizeBytes(p)
		if st := parseJobTime(p.StartedAt); !st.IsZero() && (start.IsZero() || st.Before(start)) {
			start = st
		}
		if fin := parseJobTime(p.CompletedAt); fin.After(finish) {
			finish = fin
		}
	}
	item.JobIDs = ids
	item.SizeBytes = size
	item.StartedAt = start
	if !finish.IsZero() {
		item.FinishedAt = finish
	}
	return item
}

// jobSizeBytes prefers the known download size, falling back to bytes
// downloaded so far when the size is unknown.
func jobSizeBytes(job Job) int64 {
	if job.SizeBytes.Valid {
		return job.SizeBytes.Int64
	}
	return job.BytesDone
}

// jobFinishedAt is completed_at for completed/extract_failed jobs (when
// set), else now: failed/retrying jobs have no completed_at.
func jobFinishedAt(job Job, event notify.Event) time.Time {
	if event == notify.EventCompleted || event == notify.EventExtractFailed {
		if t := parseJobTime(job.CompletedAt); !t.IsZero() {
			return t
		}
	}
	return time.Now()
}

// parseJobTime parses a stored RFC3339 timestamp, returning the zero
// time for unset or malformed values.
func parseJobTime(v sql.NullString) time.Time {
	if !v.Valid || v.String == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func isMultipartSiblingBlocking(job Job) bool {
	switch job.Status {
	case StatusQueued, StatusResolving, StatusDownloading, StatusPaused:
		return true
	case StatusFailed:
		// Treat any failed sibling as blocking so multipart extraction does not start with
		// a permanently failed/missing part (for example quota 509 after max attempts).
		return true
	default:
		return false
	}
}

func (r *Runner) deleteArchiveFiles(ctx context.Context, task decryptTask) {
	if task.archivePath == "" {
		return
	}
	paths := map[string]struct{}{task.archivePath: {}}
	groupKey, groupExplicit := multipartArchiveGroupKey(task.archivePath)
	if groupKey != "" && groupExplicit {
		if jobs, err := r.Store.ListJobs(ctx, "", false); err == nil {
			for _, j := range jobs {
				if j.OutDir != task.outDir {
					continue
				}
				p := archivePathForJob(j)
				if p == "" {
					continue
				}
				if k, _ := multipartArchiveGroupKey(p); k == groupKey {
					paths[p] = struct{}{}
				}
			}
		}
	}
	var removed int
	for p := range paths {
		if err := os.Remove(p); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				_ = r.Store.AddEvent(ctx, task.jobID, "error", "archive cleanup failed: "+filepath.Base(p)+": "+err.Error())
			}
			continue
		}
		removed++
	}
	if removed > 0 {
		_ = r.Store.AddEvent(ctx, task.jobID, "info", "archive cleanup: removed "+strconv.Itoa(removed)+" file(s)")
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
		name = archiveFilenameFromURL(job.URL)
	}
	if name == "" {
		return ""
	}
	return filepath.Join(job.OutDir, name)
}

func archiveFilenameFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	base := filepath.Base(strings.TrimSpace(parsed.Path))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	if unescaped, err := url.PathUnescape(base); err == nil {
		base = unescaped
	}
	return sanitizeFilename(base)
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
