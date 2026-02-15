package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	downloadclient "github.com/Witriol/dlq-download-queue/internal/downloader"
)

type serviceTestDownloader struct {
	pauseErr   error
	unpauseErr error
	removeErr  error
}

func (d *serviceTestDownloader) AddURI(ctx context.Context, uri string, options map[string]string) (string, error) {
	return "", nil
}

func (d *serviceTestDownloader) TellStatus(ctx context.Context, gid string) (*downloadclient.Status, error) {
	return nil, nil
}

func (d *serviceTestDownloader) Pause(ctx context.Context, gid string) error {
	return d.pauseErr
}

func (d *serviceTestDownloader) Unpause(ctx context.Context, gid string) error {
	return d.unpauseErr
}

func (d *serviceTestDownloader) Remove(ctx context.Context, gid string) error {
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
