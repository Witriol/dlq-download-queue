package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Witriol/dlq-download-queue/internal/db"
	"github.com/Witriol/dlq-download-queue/internal/series"
)

type emptyTVMaze struct{}

func (emptyTVMaze) SearchShows(context.Context, string) ([]series.TVMazeSearchResult, error) {
	return []series.TVMazeSearchResult{}, nil
}

func (emptyTVMaze) Episodes(context.Context, int64) ([]series.TVMazeEpisode, error) {
	return []series.TVMazeEpisode{}, nil
}

func newSeriesServer(t *testing.T) *Server {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "api-series.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Server{Series: &series.Manager{
		Store: series.NewStore(conn), TVMaze: emptyTVMaze{}, AllowedRoots: []string{"/data"},
	}}
}

func TestSeriesPreviewAndCreateEndpoints(t *testing.T) {
	server := newSeriesServer(t)
	previewBody := `{"reference_url":"https://webshare.cz/#/file/abcde","reference_filename":"Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv","out_dir":"/data/tv"}`
	preview := httptest.NewRecorder()
	server.Handler().ServeHTTP(preview, httptest.NewRequest(http.MethodPost, "/series/preview", strings.NewReader(previewBody)))
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", preview.Code, preview.Body.String())
	}
	var previewJSON map[string]any
	if err := json.Unmarshal(preview.Body.Bytes(), &previewJSON); err != nil {
		t.Fatal(err)
	}
	if previewJSON["reference_webshare_ident"] != "abcde" || previewJSON["quality_profile"] == nil {
		t.Fatalf("unexpected preview response: %#v", previewJSON)
	}

	createBody := `{"reference_url":"https://webshare.cz/#/file/abcde","reference_filename":"Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv","out_dir":"/data/tv","tvmaze_id":42,"display_name":"Some Show","start_mode":"template","fallback_policy":"strict"}`
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(createBody)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var watch series.WatchView
	if err := json.Unmarshal(created.Body.Bytes(), &watch); err != nil {
		t.Fatal(err)
	}
	if watch.ID <= 0 || watch.DisplayName != "Some Show" || watch.OutDir != "/data/tv" || watch.SeriesFolder != "some-show" || !watch.OrganizeBySeason {
		t.Fatalf("unexpected created watch: %+v", watch)
	}

	listed := httptest.NewRecorder()
	server.Handler().ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/series", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"display_name":"Some Show"`) {
		t.Fatalf("list status = %d body=%s", listed.Code, listed.Body.String())
	}
}

func TestSeriesActionEndpoints(t *testing.T) {
	server := newSeriesServer(t)
	createBody := `{"reference_url":"https://webshare.cz/#/file/abcde","reference_filename":"Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv","out_dir":"/data/tv","tvmaze_id":42,"display_name":"Some Show"}`
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(createBody)))
	var watch series.WatchView
	if err := json.Unmarshal(created.Body.Bytes(), &watch); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pause", "resume"} {
		recorder := httptest.NewRecorder()
		path := "/series/" + jsonNumber(watch.ID) + "/" + action
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", action, recorder.Code, recorder.Body.String())
		}
	}
	removed := httptest.NewRecorder()
	server.Handler().ServeHTTP(removed, httptest.NewRequest(http.MethodPost, "/series/"+jsonNumber(watch.ID)+"/remove", strings.NewReader("{}")))
	if removed.Code != http.StatusOK {
		t.Fatalf("remove status = %d body=%s", removed.Code, removed.Body.String())
	}
}

func TestSeriesValidationAndPausedCheckStatuses(t *testing.T) {
	server := newSeriesServer(t)
	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(`{"out_dir":"","tvmaze_id":42,"display_name":"Some Show"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("empty out_dir status = %d body=%s", invalid.Code, invalid.Body.String())
	}
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(`{"reference_url":"https://webshare.cz/#/file/abcde","reference_filename":"Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv","out_dir":"/data/tv","tvmaze_id":42,"display_name":"Some Show"}`)))
	var watch series.WatchView
	if err := json.Unmarshal(created.Body.Bytes(), &watch); err != nil {
		t.Fatal(err)
	}
	pause := httptest.NewRecorder()
	server.Handler().ServeHTTP(pause, httptest.NewRequest(http.MethodPost, "/series/"+jsonNumber(watch.ID)+"/pause", nil))
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d body=%s", pause.Code, pause.Body.String())
	}
	checkNow := httptest.NewRecorder()
	server.Handler().ServeHTTP(checkNow, httptest.NewRequest(http.MethodPost, "/series/"+jsonNumber(watch.ID)+"/check-now", nil))
	if checkNow.Code != http.StatusConflict {
		t.Fatalf("paused check-now status = %d body=%s", checkNow.Code, checkNow.Body.String())
	}
}

func TestSeriesAttentionEndpointsExposeOnlyPersistedCandidates(t *testing.T) {
	server := newSeriesServer(t)
	createBody := `{"reference_url":"https://webshare.cz/#/file/abcde","reference_filename":"Some.Show.S01E01.1080p.WEB-DL.x265-MeGusta.mkv","out_dir":"/data/tv","tvmaze_id":42,"display_name":"Some Show","fallback_policy":"manual"}`
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(createBody)))
	var watch series.WatchView
	if err := json.Unmarshal(created.Body.Bytes(), &watch); err != nil {
		t.Fatal(err)
	}
	ep, err := server.Series.Store.UpsertEpisode(context.Background(), series.EpisodeInput{WatchID: watch.ID, TVMazeEpisodeID: 1002, Season: 1, Episode: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Series.Store.SetEpisodeAttention(context.Background(), ep.ID, `[{"ident":"candidate","filename":"Some.Show.S01E02.mkv","accepted":true}]`); err != nil {
		t.Fatal(err)
	}
	attention := httptest.NewRecorder()
	server.Handler().ServeHTTP(attention, httptest.NewRequest(http.MethodGet, "/series/"+jsonNumber(watch.ID)+"/attention", nil))
	if attention.Code != http.StatusOK || !strings.Contains(attention.Body.String(), `"ident":"candidate"`) {
		t.Fatalf("attention status=%d body=%s", attention.Code, attention.Body.String())
	}
	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/series/"+jsonNumber(watch.ID)+"/episodes/"+jsonNumber(ep.ID)+"/select", strings.NewReader(`{"ident":"untrusted"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid selection status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func jsonNumber(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}
