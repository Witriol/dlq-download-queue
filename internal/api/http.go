package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Witriol/my-downloader/internal/queue"
)

type Queue interface {
	CreateJob(ctx context.Context, url, outDir, name, site string, maxAttempts int) (int64, error)
	ListJobs(ctx context.Context, status string, includeDeleted bool) ([]JobView, error)
	GetJob(ctx context.Context, id int64) (*JobView, error)
	ListEvents(ctx context.Context, id int64, limit int) ([]string, error)
	Retry(ctx context.Context, id int64) error
	Remove(ctx context.Context, id int64) error
	Clear(ctx context.Context) error
	Pause(ctx context.Context, id int64) error
	Resume(ctx context.Context, id int64) error
}

type JobView = queue.JobView

type Server struct {
	Queue Queue
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs", s.handleJobs)
	mux.HandleFunc("/jobs/clear", s.handleJobsClear)
	mux.HandleFunc("/jobs/", s.handleJob)
	return mux
}

type addJobRequest struct {
	URL         string `json:"url"`
	OutDir      string `json:"out_dir"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	MaxAttempts int    `json:"max_attempts"`
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		includeDeleted := r.URL.Query().Get("include_deleted") == "1"
		jobs, err := s.Queue.ListJobs(r.Context(), status, includeDeleted)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	case http.MethodPost:
		var req addJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if req.URL == "" || req.OutDir == "" {
			writeErr(w, http.StatusBadRequest, errors.New("missing url or out_dir"))
			return
		}
			id, err := s.Queue.CreateJob(r.Context(), req.URL, req.OutDir, req.Name, req.Site, req.MaxAttempts)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			log.Printf("action=add id=%d url=%q out=%q name=%q site=%q max_attempts=%d", id, req.URL, req.OutDir, req.Name, req.Site, req.MaxAttempts)
			writeJSON(w, http.StatusOK, map[string]any{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/jobs/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		job, err := s.Queue.GetJob(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
		return
	}
	switch parts[1] {
	case "events":
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil {
				limit = parsed
			}
		}
		events, err := s.Queue.ListEvents(r.Context(), id, limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
		case "retry":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if err := s.Queue.Retry(r.Context(), id); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			log.Printf("action=retry id=%d", id)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "remove":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if err := s.Queue.Remove(r.Context(), id); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			log.Printf("action=remove id=%d", id)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "pause":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if err := s.Queue.Pause(r.Context(), id); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			log.Printf("action=pause id=%d", id)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "resume":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if err := s.Queue.Resume(r.Context(), id); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			log.Printf("action=resume id=%d", id)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
}

func (s *Server) handleJobsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.Queue.Clear(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("action=clear")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
