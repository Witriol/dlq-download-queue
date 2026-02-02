package queue

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Witriol/my-downloader/internal/db"
	"github.com/Witriol/my-downloader/internal/downloader"
	"github.com/Witriol/my-downloader/internal/resolver"
)

type fakeResolver struct{}

func (r *fakeResolver) CanHandle(rawURL string) bool { return true }
func (r *fakeResolver) Resolve(ctx context.Context, rawURL string) (*resolver.ResolvedTarget, error) {
	return &resolver.ResolvedTarget{
		Kind:     "aria2",
		URL:      "https://example.com/file.bin",
		Filename: "file.bin",
		Size:     10,
	}, nil
}

type fakeDownloader struct {
	status *downloader.Status
}

func (d *fakeDownloader) AddURI(ctx context.Context, uri string, options map[string]string) (string, error) {
	return "gid-1", nil
}
func (d *fakeDownloader) TellStatus(ctx context.Context, gid string) (*downloader.Status, error) {
	return d.status, nil
}
func (d *fakeDownloader) Pause(ctx context.Context, gid string) error   { return nil }
func (d *fakeDownloader) Unpause(ctx context.Context, gid string) error { return nil }
func (d *fakeDownloader) Remove(ctx context.Context, gid string) error  { return nil }

func newRunnerStore(t *testing.T) *Store {
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

func TestRunnerCompletesJob(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{URL: "https://example.com/file", OutDir: "/data", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	reg := resolver.NewRegistry(&fakeResolver{})
	runner := &Runner{
		Store:       store,
		Resolvers:   reg,
		Downloader:  fakeDL,
		Concurrency: 1,
	}

	runner.tick(ctx)

	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDownloading {
		t.Fatalf("expected downloading, got %s", job.Status)
	}
	if !job.EngineGID.Valid {
		t.Fatalf("expected engine gid to be set")
	}

	fakeDL.status = &downloader.Status{
		GID:           "gid-1",
		Status:        "complete",
		TotalLength:   "10",
		CompletedLen:  "10",
		DownloadSpeed: "0",
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	job, err = store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", job.Status)
	}
	if job.BytesDone != 10 {
		t.Fatalf("expected bytes_done 10, got %d", job.BytesDone)
	}
}
