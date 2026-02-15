package queue

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Witriol/dlq-download-queue/internal/db"
	"github.com/Witriol/dlq-download-queue/internal/downloader"
	"github.com/Witriol/dlq-download-queue/internal/resolver"
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

type fakeArchiveDecryptor struct {
	mu          sync.Mutex
	attempted   bool
	err         error
	called      bool
	archivePath string
	outDir      string
	password    string
}

func (d *fakeArchiveDecryptor) MaybeDecrypt(ctx context.Context, archivePath, outDir, password string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.called = true
	d.archivePath = archivePath
	d.outDir = outDir
	d.password = password
	return d.attempted, d.err
}

func (d *fakeArchiveDecryptor) snapshot() (called bool, archivePath, outDir, password string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.called, d.archivePath, d.outDir, d.password
}

type blockingArchiveDecryptor struct {
	started chan struct{}
	release chan struct{}
}

func (d *blockingArchiveDecryptor) MaybeDecrypt(ctx context.Context, archivePath, outDir, password string) (bool, error) {
	select {
	case d.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-d.release:
		return true, nil
	}
}

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

func TestRunnerDecryptsArchiveOnComplete(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/archive",
		OutDir:          "/data",
		Name:            "archive.zip",
		ArchivePassword: sqlNullString("my-secret"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		Resolvers:        resolver.NewRegistry(&fakeResolver{}),
		Downloader:       fakeDL,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
		Concurrency:      1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:           "gid-1",
		Status:        "complete",
		TotalLength:   "10",
		CompletedLen:  "10",
		DownloadSpeed: "0",
		Files: []struct {
			Path string `json:"path"`
		}{
			{Path: "/data/archive.zip"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})
	called, archivePath, outDir, password := fakeDec.snapshot()
	if !called {
		t.Fatalf("expected decryptor to be called")
	}
	if archivePath != "/data/archive.zip" {
		t.Fatalf("archive path = %q", archivePath)
	}
	if outDir != "/data" {
		t.Fatalf("out dir = %q", outDir)
	}
	if password != "my-secret" {
		t.Fatalf("password = %q", password)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", job.Status)
	}
	if job.ArchivePassword.Valid {
		t.Fatalf("expected archive password to be cleared after decrypt")
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !eventsContain(events, "download finished") {
		t.Fatalf("expected download finished event, got: %v", events)
	}
	if !eventsContain(events, "archive decrypted: archive.zip") {
		t.Fatalf("expected archive decrypted event, got: %v", events)
	}
}

func TestRunnerDecryptsArchiveOnCompleteWithoutPassword(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/archive",
		OutDir:      "/data",
		Name:        "archive.zip",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		Resolvers:        resolver.NewRegistry(&fakeResolver{}),
		Downloader:       fakeDL,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
		Concurrency:      1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:           "gid-1",
		Status:        "complete",
		TotalLength:   "10",
		CompletedLen:  "10",
		DownloadSpeed: "0",
		Files: []struct {
			Path string `json:"path"`
		}{
			{Path: "/data/archive.zip"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})
	called, archivePath, outDir, password := fakeDec.snapshot()
	if !called {
		t.Fatalf("expected decryptor to be called")
	}
	if archivePath != "/data/archive.zip" {
		t.Fatalf("archive path = %q", archivePath)
	}
	if outDir != "/data" {
		t.Fatalf("out dir = %q", outDir)
	}
	if password != "" {
		t.Fatalf("password = %q", password)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", job.Status)
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !eventsContain(events, "archive decrypted: archive.zip") {
		t.Fatalf("expected archive decrypted event, got: %v", events)
	}
}

func TestRunnerDecryptArchiveErrorMarksDecryptFailed(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/archive",
		OutDir:          "/data",
		Name:            "archive.zip",
		ArchivePassword: sqlNullString("wrong-pass"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeDec := &fakeArchiveDecryptor{
		attempted: true,
		err:       errors.New("bad password"),
	}
	runner := &Runner{
		Store:            store,
		Resolvers:        resolver.NewRegistry(&fakeResolver{}),
		Downloader:       fakeDL,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
		Concurrency:      1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:           "gid-1",
		Status:        "complete",
		TotalLength:   "10",
		CompletedLen:  "10",
		DownloadSpeed: "0",
		Files: []struct {
			Path string `json:"path"`
		}{
			{Path: "/data/archive.zip"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDecryptFail {
		t.Fatalf("expected decrypt_failed, got %s", job.Status)
	}
	if job.ArchivePassword.Valid {
		t.Fatalf("expected archive password to be cleared after decrypt failure")
	}
	if !job.ErrorCode.Valid || job.ErrorCode.String != "archive_decrypt_failed" {
		t.Fatalf("expected archive_decrypt_failed error code, got %+v", job.ErrorCode)
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !eventsContain(events, "archive decrypt failed:") {
		t.Fatalf("expected archive decrypt failed event, got: %v", events)
	}
}

func TestRunnerMarksDecryptingWhileArchiveDecryptInProgress(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/archive",
		OutDir:          "/data",
		Name:            "archive.zip",
		ArchivePassword: sqlNullString("my-secret"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	dec := &blockingArchiveDecryptor{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	runner := &Runner{
		Store:            store,
		Resolvers:        resolver.NewRegistry(&fakeResolver{}),
		Downloader:       fakeDL,
		ArchiveDecryptor: dec,
		GetAutoDecrypt:   func() bool { return true },
		Concurrency:      1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:           "gid-1",
		Status:        "complete",
		TotalLength:   "10",
		CompletedLen:  "10",
		DownloadSpeed: "0",
		Files: []struct {
			Path string `json:"path"`
		}{
			{Path: "/data/archive.zip"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	select {
	case <-dec.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("decryptor did not start")
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDecrypting {
		t.Fatalf("expected decrypting, got %s", job.Status)
	}

	close(dec.release)
	waitFor(t, 2*time.Second, func() bool {
		updated, getErr := store.GetJob(ctx, id)
		return getErr == nil && updated.Status == StatusCompleted
	})
}

func TestRunnerSkipsDecryptWhenAutoDecryptDisabled(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/archive",
		OutDir:          "/data",
		Name:            "archive.zip",
		ArchivePassword: sqlNullString("my-secret"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		Resolvers:        resolver.NewRegistry(&fakeResolver{}),
		Downloader:       fakeDL,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return false },
		Concurrency:      1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:           "gid-1",
		Status:        "complete",
		TotalLength:   "10",
		CompletedLen:  "10",
		DownloadSpeed: "0",
		Files: []struct {
			Path string `json:"path"`
		}{
			{Path: "/data/archive.zip"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	called, _, _, _ := fakeDec.snapshot()
	if called {
		t.Fatalf("expected decryptor not to be called")
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if eventsContain(events, "archive decrypted:") || eventsContain(events, "archive decrypt failed:") {
		t.Fatalf("expected no archive decrypt events, got: %v", events)
	}
}

func TestRunnerDispatchesCompletedDecryptFromWorker(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/archive",
		OutDir:          "/data",
		Name:            "archive.zip",
		ArchivePassword: sqlNullString("my-secret"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkCompleted(ctx, id); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		Resolvers:        resolver.NewRegistry(&fakeResolver{}),
		Downloader:       fakeDL,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
		Concurrency:      1,
	}

	runner.tick(ctx)
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})

	called, archivePath, _, password := fakeDec.snapshot()
	if !called {
		t.Fatalf("expected decryptor to be called")
	}
	if archivePath != "/data/archive.zip" {
		t.Fatalf("archive path = %q", archivePath)
	}
	if password != "my-secret" {
		t.Fatalf("password = %q", password)
	}
	waitFor(t, 2*time.Second, func() bool {
		current, getErr := store.GetJob(ctx, id)
		return getErr == nil && !current.ArchivePassword.Valid
	})
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job after wait: %v", err)
	}
	if job.ArchivePassword.Valid {
		t.Fatalf("expected archive password to be cleared")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

func eventsContain(events []string, contains string) bool {
	for _, e := range events {
		if strings.Contains(e, contains) {
			return true
		}
	}
	return false
}
