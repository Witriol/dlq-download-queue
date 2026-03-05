package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	downloadclient "github.com/Witriol/dlq-download-queue/internal/downloader"
)

type serviceTestDownloader struct {
	pauseErr    error
	unpauseErr  error
	removeErr   error
	pauseHits   int
	unpauseHits int
	removeHits  int
}

func (d *serviceTestDownloader) AddURI(ctx context.Context, uri string, options map[string]string) (string, error) {
	return "", nil
}

func (d *serviceTestDownloader) TellStatus(ctx context.Context, gid string) (*downloadclient.Status, error) {
	return nil, nil
}

func (d *serviceTestDownloader) Pause(ctx context.Context, gid string) error {
	d.pauseHits++
	return d.pauseErr
}

func (d *serviceTestDownloader) Unpause(ctx context.Context, gid string) error {
	d.unpauseHits++
	return d.unpauseErr
}

func (d *serviceTestDownloader) Remove(ctx context.Context, gid string) error {
	d.removeHits++
	return d.removeErr
}

func TestServicePauseMapsActionNotAllowed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{
		pauseErr: fmt.Errorf("%w: cannot be paused now", downloadclient.ErrActionNotAllowed),
	}, []string{"/data"})
	err = svc.Pause(ctx, id)
	if !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("expected ErrActionNotAllowed, got %v", err)
	}
}

func TestServicePauseMarksQueuedJobPausedWithoutEngineGID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	if err := svc.Pause(ctx, id); err != nil {
		t.Fatalf("pause: %v", err)
	}

	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusPaused {
		t.Fatalf("expected paused status, got %s", job.Status)
	}
}

func TestServicePauseUsesStoppedEventForWebshare(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://webshare.cz/#/file/abcde/test",
		OutDir:      "/data",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	if err := svc.Pause(ctx, id); err != nil {
		t.Fatalf("pause: %v", err)
	}

	events, err := store.ListEvents(ctx, id, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, line := range events {
		if strings.Contains(line, "stopped") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stopped event for webshare pause, got %v", events)
	}
}

func TestServiceResumeMapsActionNotAllowed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{
		unpauseErr: fmt.Errorf("%w: cannot be unpaused now", downloadclient.ErrActionNotAllowed),
	}, []string{"/data"})
	err = svc.Resume(ctx, id)
	if !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("expected ErrActionNotAllowed, got %v", err)
	}
}

func TestServiceResumeRequeuesOnGIDNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}
	if err := store.UpdateProgress(ctx, id, 42, StatusDownloading, 1, 1); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{
		unpauseErr: fmt.Errorf("%w: missing gid", downloadclient.ErrGIDNotFound),
	}, []string{"/data"})
	if err := svc.Resume(ctx, id); err != nil {
		t.Fatalf("resume: %v", err)
	}

	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusQueued {
		t.Fatalf("expected queued status after resume requeue, got %s", job.Status)
	}
	if job.BytesDone != 0 {
		t.Fatalf("expected bytes_done reset after requeue, got %d", job.BytesDone)
	}
}

func TestServiceResumeRequeuesWebshareWithoutUnpause(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://webshare.cz/#/file/abcde/test",
		OutDir:      "/data",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}
	if err := store.MarkPaused(ctx, id); err != nil {
		t.Fatalf("mark paused: %v", err)
	}
	if err := store.UpdateProgress(ctx, id, 42, StatusPaused, 0, 0); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	dl := &serviceTestDownloader{}
	svc := NewService(store, dl, []string{"/data"})
	if err := svc.Resume(ctx, id); err != nil {
		t.Fatalf("resume: %v", err)
	}

	if dl.unpauseHits != 0 {
		t.Fatalf("expected unpause not to be called for webshare resume, got %d", dl.unpauseHits)
	}
	if dl.removeHits != 1 {
		t.Fatalf("expected remove to be called once for webshare resume, got %d", dl.removeHits)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusQueued {
		t.Fatalf("expected queued status after webshare resume requeue, got %s", job.Status)
	}
	if job.BytesDone != 0 {
		t.Fatalf("expected bytes_done reset after webshare resume requeue, got %d", job.BytesDone)
	}
}

func TestServiceCreateJobStoresArchivePassword(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})

	id, err := svc.CreateJob(ctx, "https://example.com/file", "/data", "file.zip", "http", "pw-123", 2)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if !job.ArchivePassword.Valid || job.ArchivePassword.String != "pw-123" {
		t.Fatalf("expected archive password to be stored")
	}
	lines, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	foundMasked := false
	for _, line := range lines {
		if strings.Contains(line, "archive_password=***") {
			foundMasked = true
		}
		if strings.Contains(line, "pw-123") {
			t.Fatalf("expected password to be masked in events")
		}
	}
	if !foundMasked {
		t.Fatalf("expected masked archive password marker in event log")
	}
}

func TestServiceCreateJobRedactsURLFragmentInEvents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})

	id, err := svc.CreateJob(ctx, "https://mega.nz/file/AbCdEf12#super-secret-key", "/data", "file.bin", "mega", "", 2)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	lines, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	foundRedacted := false
	for _, line := range lines {
		if strings.Contains(line, "#super-secret-key") {
			t.Fatalf("expected URL fragment to be redacted in events")
		}
		if strings.Contains(line, "https://mega.nz/file/AbCdEf12#***") {
			foundRedacted = true
		}
	}
	if !foundRedacted {
		t.Fatalf("expected redacted mega URL in event logs, got %v", lines)
	}
}

func TestServiceRemoveMapsActionNotAllowed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{
		removeErr: fmt.Errorf("%w: cannot be removed now", downloadclient.ErrActionNotAllowed),
	}, []string{"/data"})
	err = svc.Remove(ctx, id)
	if !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("expected ErrActionNotAllowed, got %v", err)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDownloading {
		t.Fatalf("expected job to remain downloading, got %s", job.Status)
	}
}

func TestServiceRemoveIgnoresGIDNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{
		removeErr: fmt.Errorf("%w: missing gid", downloadclient.ErrGIDNotFound),
	}, []string{"/data"})
	if err := svc.Remove(ctx, id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDeleted {
		t.Fatalf("expected deleted status, got %s", job.Status)
	}
}

func TestServiceRetryDecryptFailedQueuesDecryptOnly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/file",
		OutDir:      "/data",
		Name:        "archive.zip",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDecryptFailed(ctx, id, "archive decrypt failed"); err != nil {
		t.Fatalf("mark decrypt failed: %v", err)
	}
	dl := &serviceTestDownloader{}
	svc := NewService(store, dl, []string{"/data"})

	if err := svc.Retry(ctx, id); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if dl.removeHits != 0 {
		t.Fatalf("expected downloader remove not to be called, got %d", dl.removeHits)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDecrypting {
		t.Fatalf("expected decrypting status, got %s", job.Status)
	}
	if !job.ErrorCode.Valid || job.ErrorCode.String != "archive_decrypt_failed" {
		t.Fatalf("expected archive_decrypt_failed code to be preserved for retry routing, got %+v", job.ErrorCode)
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, line := range events {
		if strings.Contains(line, "retry decrypt queued") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected retry decrypt queued event, got %v", events)
	}
}

func TestServiceRetryResetsAttemptsForFailedJob(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/file",
		OutDir:      "/data",
		Name:        "file.bin",
		MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}
	if err := store.MarkFailed(ctx, id, "quota_exceeded", "quota exceeded; retry later", time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	failedJob, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if failedJob.Attempts == 0 {
		t.Fatalf("expected failed job to have non-zero attempts before retry")
	}

	dl := &serviceTestDownloader{}
	svc := NewService(store, dl, []string{"/data"})
	if err := svc.Retry(ctx, id); err != nil {
		t.Fatalf("retry: %v", err)
	}

	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get retried job: %v", err)
	}
	if job.Status != StatusQueued {
		t.Fatalf("expected queued status after retry, got %s", job.Status)
	}
	if job.Attempts != 0 {
		t.Fatalf("expected attempts reset to 0 after retry, got %d", job.Attempts)
	}
	if job.NextRetryAt.Valid {
		t.Fatalf("expected next_retry_at cleared after retry, got %q", job.NextRetryAt.String)
	}
	if dl.removeHits != 1 {
		t.Fatalf("expected downloader remove to be called once, got %d", dl.removeHits)
	}
}

func TestServiceRetryDecryptGroupQueuesDecryptFailedParts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"
	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        "show.part1.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part2: %v", err)
	}
	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part2ID, "archive decrypt failed", "archive_decrypt_failed"); err != nil {
		t.Fatalf("mark part2 decrypt failed: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	groupID := ""
	for _, v := range views {
		if v.ID == part1ID || v.ID == part2ID {
			groupID = v.ArchiveGroupID
			break
		}
	}
	if groupID == "" {
		t.Fatalf("expected archive group id to be present")
	}

	if err := svc.RetryDecryptGroup(ctx, groupID); err != nil {
		t.Fatalf("retry decrypt group: %v", err)
	}
	part1, err := store.GetJob(ctx, part1ID)
	if err != nil {
		t.Fatalf("get part1: %v", err)
	}
	if part1.Status != StatusCompleted {
		t.Fatalf("expected part1 to stay completed, got %s", part1.Status)
	}
	part2, err := store.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get part2: %v", err)
	}
	if part2.Status != StatusDecrypting {
		t.Fatalf("expected part2 decrypting, got %s", part2.Status)
	}
	events, err := store.ListEvents(ctx, part2ID, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, line := range events {
		if strings.Contains(line, "retry decrypt queued (group)") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected group retry event, got %v", events)
	}
}

func TestServiceRetryDecryptGroupBlockedByFailedDownload(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"
	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        "show.part1.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part2: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part1ID, "archive decrypt failed", "archive_decrypt_failed"); err != nil {
		t.Fatalf("mark part1 decrypt failed: %v", err)
	}
	if err := store.MarkFailed(ctx, part2ID, "quota_exceeded", "quota exceeded; retry later", time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatalf("mark part2 failed: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	groupID := ""
	for _, v := range views {
		if v.ID == part1ID || v.ID == part2ID {
			groupID = v.ArchiveGroupID
			break
		}
	}
	if groupID == "" {
		t.Fatalf("expected archive group id to be present")
	}

	err = svc.RetryDecryptGroup(ctx, groupID)
	if err == nil || !strings.Contains(err.Error(), "failed downloads") {
		t.Fatalf("expected failed download blocker error, got %v", err)
	}
	part1, err := store.GetJob(ctx, part1ID)
	if err != nil {
		t.Fatalf("get part1: %v", err)
	}
	if part1.Status != StatusDecryptFail {
		t.Fatalf("expected part1 to remain decrypt_failed, got %s", part1.Status)
	}
}

func TestServiceRemoveGroupRemovesAllJobs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"
	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        "show.part1.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part2: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	groupID := ""
	for _, v := range views {
		if v.ID == part1ID || v.ID == part2ID {
			groupID = v.ArchiveGroupID
			break
		}
	}
	if groupID == "" {
		t.Fatalf("expected archive group id to be present")
	}

	if err := svc.RemoveGroup(ctx, groupID); err != nil {
		t.Fatalf("remove group: %v", err)
	}
	part1, err := store.GetJob(ctx, part1ID)
	if err != nil {
		t.Fatalf("get part1: %v", err)
	}
	part2, err := store.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get part2: %v", err)
	}
	if part1.Status != StatusDeleted || part2.Status != StatusDeleted {
		t.Fatalf("expected both parts deleted, got part1=%s part2=%s", part1.Status, part2.Status)
	}
}

func TestServiceRetryDecryptGroupIgnoresSupersededFailedPart(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"

	oldPart2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create old part2: %v", err)
	}
	if err := store.MarkFailed(ctx, oldPart2ID, "quota_exceeded", "quota exceeded; retry later", time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatalf("mark old part2 failed: %v", err)
	}

	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        "show.part1.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part1ID, "archive decrypt failed", "archive_decrypt_failed"); err != nil {
		t.Fatalf("mark part1 decrypt failed: %v", err)
	}

	newPart2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create new part2: %v", err)
	}
	if err := store.MarkCompleted(ctx, newPart2ID); err != nil {
		t.Fatalf("mark new part2 completed: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}

	var (
		groupID       string
		oldPart2Group string
	)
	for _, v := range views {
		if v.ID == part1ID {
			groupID = v.ArchiveGroupID
		}
		if v.ID == oldPart2ID {
			oldPart2Group = v.ArchiveGroupID
		}
	}
	if groupID == "" {
		t.Fatalf("expected archive group id for latest multipart jobs")
	}
	if oldPart2Group != "" {
		t.Fatalf("expected superseded old part2 to be ungrouped, got %q", oldPart2Group)
	}

	if err := svc.RetryDecryptGroup(ctx, groupID); err != nil {
		t.Fatalf("retry decrypt group: %v", err)
	}
	part1, err := store.GetJob(ctx, part1ID)
	if err != nil {
		t.Fatalf("get part1: %v", err)
	}
	if part1.Status != StatusDecrypting {
		t.Fatalf("expected part1 decrypting, got %s", part1.Status)
	}
}

func TestServiceListJobsGroupsRStyleMultipart(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"

	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.rar",
		OutDir:      outDir,
		Name:        "show.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create rstyle part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.r00",
		OutDir:      outDir,
		Name:        "show.r00",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create rstyle part2: %v", err)
	}
	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := store.MarkCompleted(ctx, part2ID); err != nil {
		t.Fatalf("mark part2 completed: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}

	var part1, part2 JobView
	foundPart1 := false
	foundPart2 := false
	for _, v := range views {
		if v.ID == part1ID {
			part1 = v
			foundPart1 = true
		}
		if v.ID == part2ID {
			part2 = v
			foundPart2 = true
		}
	}
	if !foundPart1 || !foundPart2 {
		t.Fatalf("expected both rstyle jobs in list")
	}
	if !part1.ArchiveIsMultipart || !part2.ArchiveIsMultipart {
		t.Fatalf("expected rstyle jobs to be marked multipart, got part1=%v part2=%v", part1.ArchiveIsMultipart, part2.ArchiveIsMultipart)
	}
	if part1.ArchiveGroupID == "" || part2.ArchiveGroupID == "" || part1.ArchiveGroupID != part2.ArchiveGroupID {
		t.Fatalf("expected same non-empty archive_group_id, got part1=%q part2=%q", part1.ArchiveGroupID, part2.ArchiveGroupID)
	}
	if part1.ArchivePartNumber != 1 || part2.ArchivePartNumber != 2 {
		t.Fatalf("expected rstyle part numbers 1/2, got %d/%d", part1.ArchivePartNumber, part2.ArchivePartNumber)
	}
}

func TestServiceListJobsPreservesGroupInFilteredView(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"
	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        "show.part1.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part2: %v", err)
	}
	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part2ID, "archive decrypt failed", "archive_decrypt_failed"); err != nil {
		t.Fatalf("mark part2 decrypt failed: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, StatusDecryptFail, false)
	if err != nil {
		t.Fatalf("list decrypt_failed jobs: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("expected one decrypt_failed job, got %d", len(views))
	}
	if views[0].ID != part2ID {
		t.Fatalf("expected part2 in filtered view, got id=%d", views[0].ID)
	}
	if !views[0].ArchiveIsMultipart || views[0].ArchiveGroupID == "" {
		t.Fatalf("expected grouped metadata in filtered view, got multipart=%v group=%q", views[0].ArchiveIsMultipart, views[0].ArchiveGroupID)
	}
}

func TestServiceGetJobIncludesArchiveGroupFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"
	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        "show.part1.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part2.rar",
		OutDir:      outDir,
		Name:        "show.part2.rar",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part2: %v", err)
	}
	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part2ID, "archive decrypt failed", "archive_decrypt_failed"); err != nil {
		t.Fatalf("mark part2 decrypt failed: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	view, err := svc.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if !view.ArchiveIsMultipart || view.ArchiveGroupID == "" {
		t.Fatalf("expected grouped metadata on single-job endpoint, got multipart=%v group=%q", view.ArchiveIsMultipart, view.ArchiveGroupID)
	}
}

func TestServiceListJobsGroupsMultipartByURLFilenameHint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	outDir := "/data"

	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/files/show.part1.rar",
		OutDir:      outDir,
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/files/show.part2.rar",
		OutDir:      outDir,
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part2: %v", err)
	}

	svc := NewService(store, &serviceTestDownloader{}, []string{"/data"})
	views, err := svc.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}

	var part1, part2 JobView
	foundPart1 := false
	foundPart2 := false
	for _, v := range views {
		if v.ID == part1ID {
			part1 = v
			foundPart1 = true
		}
		if v.ID == part2ID {
			part2 = v
			foundPart2 = true
		}
	}
	if !foundPart1 || !foundPart2 {
		t.Fatalf("expected both multipart jobs in list")
	}
	if !part1.ArchiveIsMultipart || !part2.ArchiveIsMultipart {
		t.Fatalf("expected URL-derived jobs to be grouped, got part1=%v part2=%v", part1.ArchiveIsMultipart, part2.ArchiveIsMultipart)
	}
	if part1.ArchiveGroupID == "" || part2.ArchiveGroupID == "" || part1.ArchiveGroupID != part2.ArchiveGroupID {
		t.Fatalf("expected same non-empty archive_group_id, got part1=%q part2=%q", part1.ArchiveGroupID, part2.ArchiveGroupID)
	}
}
