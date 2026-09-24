package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Witriol/dlq-download-queue/internal/series"
)

const (
	defaultSeriesEventLimit = 50
	maxSeriesEventLimit     = 500
)

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if s.Series == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("series watcher not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.Series.List(r.Context())
		if err != nil {
			writeSeriesErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req series.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.Series.Create(r.Context(), req)
		if err != nil {
			writeSeriesErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSeriesPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Series == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("series watcher not configured"))
		return
	}
	var req series.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	preview, err := s.Series.Preview(r.Context(), req)
	if err != nil {
		writeSeriesErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleTVMazeSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Series == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("series watcher not configured"))
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeErr(w, http.StatusBadRequest, errors.New("q is required"))
		return
	}
	results, err := s.Series.SearchShows(r.Context(), query)
	if err != nil {
		writeSeriesErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSeriesItem(w http.ResponseWriter, r *http.Request) {
	if s.Series == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("series watcher not configured"))
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/series/"), "/"), "/")
	if len(parts) < 1 || len(parts) > 4 || parts[0] == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("invalid series id"))
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			item, err := s.Series.Get(r.Context(), id)
			if err != nil {
				writeSeriesErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			var req series.UpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			item, err := s.Series.Update(r.Context(), id, req)
			if err != nil {
				writeSeriesErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			if err := s.Series.Remove(r.Context(), id); err != nil {
				writeSeriesErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 2 && parts[1] == "attention" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		items, err := s.Series.ListAttention(r.Context(), id)
		if err != nil {
			writeSeriesErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		events, err := s.Series.ListEvents(r.Context(), id, seriesEventLimit(r.URL.Query().Get("limit")))
		if err != nil {
			writeSeriesErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
		return
	}
	if len(parts) == 4 && parts[1] == "episodes" && parts[3] == "select" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		episodeID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || episodeID <= 0 {
			writeErr(w, http.StatusBadRequest, errors.New("invalid episode id"))
			return
		}
		var req series.SelectAttentionCandidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.Series.SelectAttentionCandidate(r.Context(), id, episodeID, req.Ident)
		if err != nil {
			writeSeriesErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var item any
	switch parts[1] {
	case "check-now":
		item, err = s.Series.CheckNow(r.Context(), id)
	case "pause":
		item, err = s.Series.SetEnabled(r.Context(), id, false)
	case "resume":
		item, err = s.Series.SetEnabled(r.Context(), id, true)
	case "remove":
		err = s.Series.Remove(r.Context(), id)
		item = map[string]string{"status": "ok"}
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		writeSeriesErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func seriesEventLimit(raw string) int {
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return defaultSeriesEventLimit
	}
	if limit < 1 {
		return 1
	}
	if limit > maxSeriesEventLimit {
		return maxSeriesEventLimit
	}
	return limit
}

func writeSeriesErr(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, series.ErrAttentionSelectionConflict) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	if errors.Is(err, series.ErrWatchPaused) || errors.Is(err, series.ErrWatchCheckInProgress) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	var validation *series.ValidationError
	if errors.As(err, &validation) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeErr(w, http.StatusBadGateway, err)
}
