package queue

import (
	"context"
	"errors"
	"os"
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

type fakeMegaDecryptor struct {
	mu       sync.Mutex
	attempt  bool
	err      error
	called   bool
	site     string
	rawURL   string
	filePath string
}

func (d *fakeMegaDecryptor) MaybeDecrypt(ctx context.Context, site, rawURL, filePath string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.called = true
	d.site = site
	d.rawURL = rawURL
	d.filePath = filePath
	return d.attempt, d.err
}

func (d *fakeMegaDecryptor) snapshot() (called bool, site, rawURL, filePath string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.called, d.site, d.rawURL, d.filePath
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

func TestRunnerPrepareOutputForStartSkipsControlCleanupWhenOutputInUse(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	outDir := t.TempDir()

	activeID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/active",
		OutDir:      outDir,
		Name:        "shared.bin",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create active job: %v", err)
	}
	if err := store.MarkDownloading(ctx, activeID, "aria2", "gid-active"); err != nil {
		t.Fatalf("mark active downloading: %v", err)
	}

	currentID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/current",
		OutDir:      outDir,
		Name:        "shared.bin",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create current job: %v", err)
	}
	currentJob, err := store.GetJob(ctx, currentID)
	if err != nil {
		t.Fatalf("get current job: %v", err)
	}

	controlPath := filepath.Join(outDir, "shared.bin.aria2")
	if err := os.WriteFile(controlPath, []byte("state"), 0o644); err != nil {
		t.Fatalf("write control file: %v", err)
	}

	runner := &Runner{Store: store}
	if err := runner.prepareOutputForStart(ctx, currentJob, "shared.bin", map[string]string{"continue": "false"}); err != nil {
		t.Fatalf("prepare output: %v", err)
	}
	if _, err := os.Stat(controlPath); err != nil {
		t.Fatalf("expected control file to stay in place, stat err: %v", err)
	}
}

func TestRunnerPrepareOutputForStartRemovesControlFileWithoutConflict(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	outDir := t.TempDir()

	currentID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/current",
		OutDir:      outDir,
		Name:        "solo.bin",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create current job: %v", err)
	}
	currentJob, err := store.GetJob(ctx, currentID)
	if err != nil {
		t.Fatalf("get current job: %v", err)
	}

	controlPath := filepath.Join(outDir, "solo.bin.aria2")
	if err := os.WriteFile(controlPath, []byte("state"), 0o644); err != nil {
		t.Fatalf("write control file: %v", err)
	}

	runner := &Runner{Store: store}
	if err := runner.prepareOutputForStart(ctx, currentJob, "solo.bin", map[string]string{"continue": "false"}); err != nil {
		t.Fatalf("prepare output: %v", err)
	}
	if _, err := os.Stat(controlPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected control file to be removed, err: %v", err)
	}
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
	waitFor(t, 2*time.Second, func() bool {
		current, getErr := store.GetJob(ctx, id)
		return getErr == nil && current.Status == StatusCompleted
	})
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

func TestRunnerLogsDownloadErrorEvent(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/file",
		OutDir:      "/data",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	runner := &Runner{
		Store:       store,
		Resolvers:   resolver.NewRegistry(&fakeResolver{}),
		Downloader:  fakeDL,
		Concurrency: 1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:          "gid-1",
		Status:       "error",
		ErrorMessage: "The response status is not successful. status=500",
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", job.Status)
	}
	if !job.ErrorCode.Valid || job.ErrorCode.String != "download_error" {
		t.Fatalf("expected download_error code, got %+v", job.ErrorCode)
	}
	if !job.Error.Valid || job.Error.String != "The response status is not successful. status=500" {
		t.Fatalf("unexpected error message: %+v", job.Error)
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !eventsContain(events, "The response status is not successful. status=500") {
		t.Fatalf("expected download error event, got: %v", events)
	}
}

func TestRunnerMaps509ToQuotaExceeded(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/file",
		OutDir:      "/data",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	runner := &Runner{
		Store:       store,
		Resolvers:   resolver.NewRegistry(&fakeResolver{}),
		Downloader:  fakeDL,
		Concurrency: 1,
	}

	runner.tick(ctx)
	fakeDL.status = &downloader.Status{
		GID:          "gid-1",
		Status:       "error",
		ErrorMessage: "The response status is not successful. status=509",
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", job.Status)
	}
	if !job.ErrorCode.Valid || job.ErrorCode.String != "quota_exceeded" {
		t.Fatalf("expected quota_exceeded code, got %+v", job.ErrorCode)
	}
	if !job.NextRetryAt.Valid {
		t.Fatalf("expected next_retry_at to be set")
	}
	nextRetry, err := time.Parse(time.RFC3339, job.NextRetryAt.String)
	if err != nil {
		t.Fatalf("parse next_retry_at: %v", err)
	}
	if time.Until(nextRetry) < 90*time.Minute {
		t.Fatalf("expected retry delay near 2h, got next_retry_at=%s", job.NextRetryAt.String)
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
	waitFor(t, 2*time.Second, func() bool {
		current, getErr := store.GetJob(ctx, id)
		return getErr == nil && current.Status == StatusCompleted
	})
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

func TestRunnerMegaDecryptsOnComplete(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	url := "https://mega.nz/file/AbCdEf12#QwErTy123_-"
	id, err := store.CreateJob(ctx, &Job{
		URL:         url,
		OutDir:      "/data",
		Name:        "file.bin",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeMega := &fakeMegaDecryptor{attempt: true}
	runner := &Runner{
		Store:         store,
		Resolvers:     resolver.NewRegistry(&fakeResolver{}),
		Downloader:    fakeDL,
		MegaDecryptor: fakeMega,
		GetAutoDecrypt: func() bool {
			return false
		},
		Concurrency: 1,
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
			{Path: "/data/file.bin"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeMega.snapshot()
		return called
	})
	called, site, rawURL, filePath := fakeMega.snapshot()
	if !called {
		t.Fatalf("expected mega decryptor to be called")
	}
	if site != "" {
		t.Fatalf("site = %q", site)
	}
	if rawURL != url {
		t.Fatalf("rawURL = %q", rawURL)
	}
	if filePath != "/data/file.bin" {
		t.Fatalf("filePath = %q", filePath)
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
	if !eventsContain(events, "mega decrypt started: file.bin") {
		t.Fatalf("expected mega decrypt started event, got: %v", events)
	}
	if !eventsContain(events, "mega decrypted: file.bin") {
		t.Fatalf("expected mega decrypted event, got: %v", events)
	}
}

func TestRunnerMegaDecryptErrorMarksDecryptFailed(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	id, err := store.CreateJob(ctx, &Job{
		URL:         "https://mega.nz/file/AbCdEf12#QwErTy123_-",
		OutDir:      "/data",
		Name:        "file.bin",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	fakeDL := &fakeDownloader{}
	fakeMega := &fakeMegaDecryptor{
		attempt: true,
		err:     errors.New("mac mismatch"),
	}
	runner := &Runner{
		Store:         store,
		Resolvers:     resolver.NewRegistry(&fakeResolver{}),
		Downloader:    fakeDL,
		MegaDecryptor: fakeMega,
		Concurrency:   1,
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
			{Path: "/data/file.bin"},
		},
	}

	if err := runner.updateActive(ctx); err != nil {
		t.Fatalf("updateActive: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeMega.snapshot()
		return called
	})
	job, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusDecryptFail {
		t.Fatalf("expected decrypt_failed, got %s", job.Status)
	}
	if !job.ErrorCode.Valid || job.ErrorCode.String != "mega_decrypt_failed" {
		t.Fatalf("expected mega_decrypt_failed error code, got %+v", job.ErrorCode)
	}
	events, err := store.ListEvents(ctx, id, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !eventsContain(events, "mega decrypt failed:") {
		t.Fatalf("expected mega decrypt failed event, got: %v", events)
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

func TestRunnerWaitsForMultipartSiblingsBeforeArchiveDecrypt(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	outDir := t.TempDir()
	part1Name := "show.part1.rar"
	part2Name := "show.part2.rar"
	part1Path := filepath.Join(outDir, part1Name)
	part2Path := filepath.Join(outDir, part2Name)
	if err := os.WriteFile(part1Path, []byte("part1"), 0o644); err != nil {
		t.Fatalf("write part1: %v", err)
	}
	if err := os.WriteFile(part2Path, []byte("part2"), 0o644); err != nil {
		t.Fatalf("write part2: %v", err)
	}

	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://example.com/show.part1.rar",
		OutDir:      outDir,
		Name:        part1Name,
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1 job: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:             "https://example.com/show.part2.rar",
		OutDir:          outDir,
		Name:            part2Name,
		ArchivePassword: sqlNullString("pw"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create part2 job: %v", err)
	}
	if err := store.MarkDownloading(ctx, part1ID, "aria2", "gid-part1"); err != nil {
		t.Fatalf("mark part1 downloading: %v", err)
	}
	if err := store.MarkDownloading(ctx, part2ID, "aria2", "gid-part2"); err != nil {
		t.Fatalf("mark part2 downloading: %v", err)
	}

	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
	}

	part2Job, err := store.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get part2 job: %v", err)
	}
	handled := runner.queueDecryptFromStatus(ctx, *part2Job, &downloader.Status{
		Files: []struct {
			Path string `json:"path"`
		}{
			{Path: part2Path},
		},
	}, 123)
	if !handled {
		t.Fatalf("expected queueDecryptFromStatus to handle multipart job")
	}
	time.Sleep(50 * time.Millisecond)
	called, _, _, _ := fakeDec.snapshot()
	if called {
		t.Fatalf("did not expect decryptor call while sibling part is still downloading")
	}
	part2State, err := store.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get part2 state: %v", err)
	}
	if part2State.Status != StatusDecrypting {
		t.Fatalf("expected part2 status decrypting while waiting, got %s", part2State.Status)
	}
	events, err := store.ListEvents(ctx, part2ID, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !eventsContain(events, "archive decrypt waiting: multipart set still downloading") {
		t.Fatalf("expected wait event, got: %v", events)
	}

	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := runner.dispatchCompletedDecrypt(ctx); err != nil {
		t.Fatalf("dispatchCompletedDecrypt: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})
	_, archivePath, _, _ := fakeDec.snapshot()
	if archivePath != part1Path {
		t.Fatalf("expected decrypt from first part %q, got %q", part1Path, archivePath)
	}
	waitFor(t, 2*time.Second, func() bool {
		updated, getErr := store.GetJob(ctx, part2ID)
		return getErr == nil && updated.Status == StatusCompleted
	})
}

func TestRunnerRetryArchiveFailureSkipsMegaDecrypt(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	outDir := t.TempDir()
	part1Name := "show.part1.rar"
	part2Name := "show.part2.rar"
	part1Path := filepath.Join(outDir, part1Name)
	part2Path := filepath.Join(outDir, part2Name)
	if err := os.WriteFile(part1Path, []byte("part1"), 0o644); err != nil {
		t.Fatalf("write part1: %v", err)
	}
	if err := os.WriteFile(part2Path, []byte("part2"), 0o644); err != nil {
		t.Fatalf("write part2: %v", err)
	}

	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://mega.nz/file/AbCdEf12#QwErTy123_-",
		OutDir:      outDir,
		Name:        part1Name,
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1 job: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:             "https://mega.nz/file/AbCdEf12#QwErTy123_-",
		OutDir:          outDir,
		Name:            part2Name,
		ArchivePassword: sqlNullString("pw"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create part2 job: %v", err)
	}
	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part2ID, "archive decrypt failed", "archive_decrypt_failed"); err != nil {
		t.Fatalf("mark part2 postprocess failed: %v", err)
	}
	if err := store.MarkDecryptingRetry(ctx, part2ID, 111); err != nil {
		t.Fatalf("mark part2 decrypting retry: %v", err)
	}

	fakeMega := &fakeMegaDecryptor{attempt: true}
	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		MegaDecryptor:    fakeMega,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
	}

	if err := runner.dispatchCompletedDecrypt(ctx); err != nil {
		t.Fatalf("dispatchCompletedDecrypt: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})
	megaCalled, _, _, megaPath := fakeMega.snapshot()
	if megaCalled {
		t.Fatalf("expected mega decrypt to be skipped, got call for %q", megaPath)
	}
	decCalled, archivePath, _, _ := fakeDec.snapshot()
	if !decCalled {
		t.Fatalf("expected archive decrypt to be called")
	}
	if archivePath != part1Path {
		t.Fatalf("expected archive path %q, got %q", part1Path, archivePath)
	}
	job, err := store.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get part2 job: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("expected part2 completed, got %s", job.Status)
	}
}

func TestRunnerRetryMegaFailureSkipsMegaDecryptForArchive(t *testing.T) {
	store := newRunnerStore(t)
	ctx := context.Background()
	outDir := t.TempDir()
	part1Name := "show.part1.rar"
	part2Name := "show.part2.rar"
	part1Path := filepath.Join(outDir, part1Name)
	part2Path := filepath.Join(outDir, part2Name)
	if err := os.WriteFile(part1Path, []byte("part1"), 0o644); err != nil {
		t.Fatalf("write part1: %v", err)
	}
	if err := os.WriteFile(part2Path, []byte("part2"), 0o644); err != nil {
		t.Fatalf("write part2: %v", err)
	}

	part1ID, err := store.CreateJob(ctx, &Job{
		URL:         "https://mega.nz/file/AbCdEf12#QwErTy123_-",
		OutDir:      outDir,
		Name:        part1Name,
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("create part1 job: %v", err)
	}
	part2ID, err := store.CreateJob(ctx, &Job{
		URL:             "https://mega.nz/file/AbCdEf12#QwErTy123_-",
		OutDir:          outDir,
		Name:            part2Name,
		ArchivePassword: sqlNullString("pw"),
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create part2 job: %v", err)
	}
	if err := store.MarkCompleted(ctx, part1ID); err != nil {
		t.Fatalf("mark part1 completed: %v", err)
	}
	if err := store.MarkPostprocessFailed(ctx, part2ID, "mega decrypt failed", "mega_decrypt_failed"); err != nil {
		t.Fatalf("mark part2 postprocess failed: %v", err)
	}
	if err := store.MarkDecryptingRetry(ctx, part2ID, 111); err != nil {
		t.Fatalf("mark part2 decrypting retry: %v", err)
	}

	fakeMega := &fakeMegaDecryptor{attempt: true}
	fakeDec := &fakeArchiveDecryptor{attempted: true}
	runner := &Runner{
		Store:            store,
		MegaDecryptor:    fakeMega,
		ArchiveDecryptor: fakeDec,
		GetAutoDecrypt:   func() bool { return true },
	}

	if err := runner.dispatchCompletedDecrypt(ctx); err != nil {
		t.Fatalf("dispatchCompletedDecrypt: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		called, _, _, _ := fakeDec.snapshot()
		return called
	})
	megaCalled, _, _, megaPath := fakeMega.snapshot()
	if megaCalled {
		t.Fatalf("expected mega decrypt to be skipped, got call for %q", megaPath)
	}
	decCalled, archivePath, _, _ := fakeDec.snapshot()
	if !decCalled {
		t.Fatalf("expected archive decrypt to be called")
	}
	if archivePath != part1Path {
		t.Fatalf("expected archive path %q, got %q", part1Path, archivePath)
	}
	job, err := store.GetJob(ctx, part2ID)
	if err != nil {
		t.Fatalf("get part2 job: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("expected part2 completed, got %s", job.Status)
	}
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

func TestRunnerDispatchSkipsCompletedNoPasswordToAvoidLoop(t *testing.T) {
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

	if err := runner.dispatchCompletedDecrypt(ctx); err != nil {
		t.Fatalf("dispatchCompletedDecrypt: %v", err)
	}
	called, _, _, _ := fakeDec.snapshot()
	if called {
		t.Fatalf("expected completed no-password archive job to be skipped by dispatch worker")
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
