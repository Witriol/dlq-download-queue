package queue

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Witriol/dlq-download-queue/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dlq.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return NewStore(conn)
}

func TestStoreRequeueClearsFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	job := &Job{
		URL:             "https://example.com/file",
		OutDir:          "/data",
		Name:            "file.bin",
		Site:            "http",
		ArchivePassword: sqlNullString("pass-1"),
		MaxAttempts:     3,
	}
	id, err := store.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.UpdateResolving(ctx, id, "https://resolved", "file.bin", 123); err != nil {
		t.Fatalf("update resolving: %v", err)
	}
	if err := store.MarkDownloading(ctx, id, "aria2", "gid-1"); err != nil {
		t.Fatalf("mark downloading: %v", err)
	}
	if err := store.UpdateProgress(ctx, id, 77, StatusDownloading, 10, 1); err != nil {
		t.Fatalf("update progress: %v", err)
	}
	if err := store.MarkFailed(ctx, id, "download_error", "fail", time.Now().UTC()); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := store.Requeue(ctx, id); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	updated, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if updated.Status != StatusQueued {
		t.Fatalf("expected status queued, got %s", updated.Status)
	}
	if updated.Engine == "" {
		t.Fatalf("expected engine to be set after requeue")
	}
	if updated.EngineGID.Valid {
		t.Fatalf("expected engine_gid to be cleared")
	}
	if updated.ResolvedURL.Valid || updated.Filename.Valid || updated.SizeBytes.Valid {
		t.Fatalf("expected resolved fields to be cleared")
	}
	if updated.BytesDone != 0 {
		t.Fatalf("expected bytes_done to be reset, got %d", updated.BytesDone)
	}
	if !updated.ArchivePassword.Valid || updated.ArchivePassword.String != "pass-1" {
		t.Fatalf("expected archive password to persist")
	}
}

func TestStoreSoftDeleteList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.Remove(ctx, id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	jobs, err := store.ListJobs(ctx, "", false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(jobs))
	}
	jobs, err = store.ListJobs(ctx, "", true)
	if err != nil {
		t.Fatalf("list jobs include deleted: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Status != StatusDeleted {
		t.Fatalf("expected deleted status, got %s", jobs[0].Status)
	}
}

func TestMarkCompletedSetsBytes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.UpdateResolving(ctx, id, "https://resolved", "file.bin", 42); err != nil {
		t.Fatalf("update resolving: %v", err)
	}
	if err := store.MarkCompleted(ctx, id); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	updated, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if updated.BytesDone != 42 {
		t.Fatalf("expected bytes_done 42, got %d", updated.BytesDone)
	}
}

func TestListRetryableFailed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	idRetry, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file1", OutDir: "/data", MaxAttempts: 2})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkFailed(ctx, idRetry, "resolve_failed", "fail", time.Now().UTC().Add(-1*time.Minute)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	idNoRetry, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file2", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkFailed(ctx, idNoRetry, "resolve_failed", "fail", time.Now().UTC().Add(-1*time.Minute)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	ids, err := store.ListRetryableFailed(ctx, 10)
	if err != nil {
		t.Fatalf("list retryable: %v", err)
	}
	foundRetry := false
	foundNoRetry := false
	for _, id := range ids {
		if id == idRetry {
			foundRetry = true
		}
		if id == idNoRetry {
			foundNoRetry = true
		}
	}
	if !foundRetry {
		t.Fatalf("expected retryable job to be listed")
	}
	if foundNoRetry {
		t.Fatalf("did not expect max-attempts job to be listed")
	}
}

func TestMarkFailedLogsMaxAttempts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkFailed(ctx, id, "download_error", "fail", time.Now().UTC()); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	lines, err := store.ListEvents(ctx, id, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, line := range lines {
		if strings.Contains(line, "max attempts reached") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected max attempts event to be logged")
	}
}

func TestListPendingArchiveDecryptAndClear(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	withPassID, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/a.zip",
		OutDir:          "/data",
		Name:            "a.zip",
		ArchivePassword: sqlNullString("pw-a"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create job with password: %v", err)
	}
	if err := store.MarkCompleted(ctx, withPassID); err != nil {
		t.Fatalf("mark completed with password: %v", err)
	}

	decryptingID, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/c.zip",
		OutDir:          "/data",
		Name:            "c.zip",
		ArchivePassword: sqlNullString("pw-c"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create decrypting job: %v", err)
	}
	if err := store.MarkDecrypting(ctx, decryptingID, 11); err != nil {
		t.Fatalf("mark decrypting: %v", err)
	}

	noPassID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/b.zip",
		OutDir:      "/data",
		Name:        "b.zip",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job without password: %v", err)
	}
	if err := store.MarkCompleted(ctx, noPassID); err != nil {
		t.Fatalf("mark completed without password: %v", err)
	}

	nonArchiveID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/d.txt",
		OutDir:      "/data",
		Name:        "d.txt",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create non-archive job: %v", err)
	}
	if err := store.MarkCompleted(ctx, nonArchiveID); err != nil {
		t.Fatalf("mark completed non-archive: %v", err)
	}

	pending, err := store.ListPendingArchiveDecrypt(ctx, 0)
	if err != nil {
		t.Fatalf("list pending archive decrypt: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending jobs, got %+v", pending)
	}
	ids := map[int64]bool{
		withPassID:   false,
		decryptingID: false,
		noPassID:     false,
	}
	for _, j := range pending {
		if _, ok := ids[j.ID]; ok {
			ids[j.ID] = true
		}
		if j.ID == nonArchiveID {
			t.Fatalf("expected non-archive job %d to be excluded from pending list", nonArchiveID)
		}
	}
	if !ids[withPassID] || !ids[decryptingID] || !ids[noPassID] {
		t.Fatalf("expected jobs %d, %d, and %d in pending list, got %+v", withPassID, decryptingID, noPassID, pending)
	}

	if err := store.ClearArchivePassword(ctx, withPassID); err != nil {
		t.Fatalf("clear archive password: %v", err)
	}
	updated, err := store.GetJob(ctx, withPassID)
	if err != nil {
		t.Fatalf("get updated job: %v", err)
	}
	if updated.ArchivePassword.Valid {
		t.Fatalf("expected archive password to be cleared")
	}
}
