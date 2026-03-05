package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Witriol/dlq-download-queue/internal/queue"
)

func TestStatusForQueueErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: sql.ErrNoRows, want: http.StatusNotFound},
		{name: "invalid out dir", err: errors.New("out_dir must be absolute"), want: http.StatusBadRequest},
		{name: "missing engine gid", err: queue.ErrMissingEngineGID, want: http.StatusConflict},
		{name: "action not allowed", err: fmt.Errorf("%w: detail", queue.ErrActionNotAllowed), want: http.StatusConflict},
		{name: "downloader missing", err: queue.ErrDownloaderNotConfigured, want: http.StatusServiceUnavailable},
		{name: "invalid archive group id", err: fmt.Errorf("%w: invalid", queue.ErrInvalidArchiveGroupID), want: http.StatusBadRequest},
		{name: "archive group blocked", err: fmt.Errorf("%w: still downloading", queue.ErrArchiveGroupBlocked), want: http.StatusConflict},
		{name: "archive group no decrypt failures", err: fmt.Errorf("%w: none", queue.ErrArchiveGroupNoDecrypt), want: http.StatusConflict},
		{name: "unknown", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		if got := statusForQueueErr(tt.err); got != tt.want {
			t.Fatalf("%s: statusForQueueErr(%v)=%d, want %d", tt.name, tt.err, got, tt.want)
		}
	}
}

type stubQueue struct {
	pauseErr              error
	retryGroupErr         error
	removeGroupErr        error
	retryGroupCalledWith  string
	removeGroupCalledWith string
}

func (q *stubQueue) CreateJob(ctx context.Context, url, outDir, name, site, archivePassword string, maxAttempts int) (int64, error) {
	return 0, nil
}

func (q *stubQueue) ListJobs(ctx context.Context, status string, includeDeleted bool) ([]JobView, error) {
	return nil, nil
}

func (q *stubQueue) GetJob(ctx context.Context, id int64) (*JobView, error) {
	return nil, nil
}

func (q *stubQueue) ListEvents(ctx context.Context, id int64, limit int) ([]string, error) {
	return nil, nil
}

func (q *stubQueue) Retry(ctx context.Context, id int64) error {
	return nil
}

func (q *stubQueue) RetryDecryptGroup(ctx context.Context, groupID string) error {
	q.retryGroupCalledWith = groupID
	return q.retryGroupErr
}

func (q *stubQueue) Remove(ctx context.Context, id int64) error {
	return nil
}

func (q *stubQueue) RemoveGroup(ctx context.Context, groupID string) error {
	q.removeGroupCalledWith = groupID
	return q.removeGroupErr
}

func (q *stubQueue) Clear(ctx context.Context) error {
	return nil
}

func (q *stubQueue) Purge(ctx context.Context) error {
	return nil
}

func (q *stubQueue) Pause(ctx context.Context, id int64) error {
	return q.pauseErr
}

func (q *stubQueue) Resume(ctx context.Context, id int64) error {
	return nil
}

func TestHandlePauseMapsActionNotAllowedToConflict(t *testing.T) {
	srv := &Server{
		Queue: &stubQueue{pauseErr: fmt.Errorf("%w: cannot be paused now", queue.ErrActionNotAllowed)},
	}
	req := httptest.NewRequest(http.MethodPost, "/jobs/123/pause", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(body["error"], "action_not_allowed") {
		t.Fatalf("expected action_not_allowed error, got %q", body["error"])
	}
}

func TestHandlePauseMapsNotFoundTo404(t *testing.T) {
	srv := &Server{
		Queue: &stubQueue{pauseErr: sql.ErrNoRows},
	}
	req := httptest.NewRequest(http.MethodPost, "/jobs/123/pause", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRedactURLForLog(t *testing.T) {
	got := redactURLForLog("https://mega.nz/file/AbCdEf12#super-secret-key")
	if got != "https://mega.nz/file/AbCdEf12#***" {
		t.Fatalf("redacted url = %q", got)
	}
	got = redactURLForLog("https://example.com/file.bin")
	if got != "https://example.com/file.bin" {
		t.Fatalf("url without fragment should be unchanged, got %q", got)
	}
}

func TestHandleGroupRetryDecrypt(t *testing.T) {
	q := &stubQueue{}
	srv := &Server{Queue: q}
	req := httptest.NewRequest(http.MethodPost, "/jobs/groups/ag_test/retry-decrypt", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if q.retryGroupCalledWith != "ag_test" {
		t.Fatalf("expected retry group id ag_test, got %q", q.retryGroupCalledWith)
	}
}

func TestHandleGroupRemove(t *testing.T) {
	q := &stubQueue{}
	srv := &Server{Queue: q}
	req := httptest.NewRequest(http.MethodPost, "/jobs/groups/ag_test/remove", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if q.removeGroupCalledWith != "ag_test" {
		t.Fatalf("expected remove group id ag_test, got %q", q.removeGroupCalledWith)
	}
}

func TestHandleGroupRetryDecryptBlockedMapsToConflict(t *testing.T) {
	q := &stubQueue{
		retryGroupErr: fmt.Errorf("%w: group still downloading; retry decrypt unavailable", queue.ErrArchiveGroupBlocked),
	}
	srv := &Server{Queue: q}
	req := httptest.NewRequest(http.MethodPost, "/jobs/groups/ag_test/retry-decrypt", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestHandleGroupRejectsExtraPathSegments(t *testing.T) {
	q := &stubQueue{}
	srv := &Server{Queue: q}
	req := httptest.NewRequest(http.MethodPost, "/jobs/groups/ag_test/remove/extra", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if q.removeGroupCalledWith != "" {
		t.Fatalf("expected remove not to be called, got group id %q", q.removeGroupCalledWith)
	}
}
