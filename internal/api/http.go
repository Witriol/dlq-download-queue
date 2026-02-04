package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Witriol/my-downloader/internal/queue"
)

const maxRequestBodyBytes = 1 << 20

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
	Queue    Queue
	Meta     *Meta
	Settings *Settings
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/meta", s.handleMeta)
	mux.HandleFunc("/jobs", s.handleJobs)
	mux.HandleFunc("/jobs/clear", s.handleJobsClear)
	mux.HandleFunc("/jobs/", s.handleJob)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/browse/mkdir", s.handleBrowseMkdir)
	mux.HandleFunc("/api/browse", s.handleBrowse)
	return withRequestLimit(mux, maxRequestBodyBytes)
}

type Meta struct {
	OutDirPresets []string `json:"out_dir_presets"`
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	meta := s.Meta
	if meta == nil {
		meta = &Meta{}
	}
	writeJSON(w, http.StatusOK, meta)
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
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeErr(w, http.StatusRequestEntityTooLarge, errors.New("request body too large"))
				return
			}
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if req.URL == "" || req.OutDir == "" {
			writeErr(w, http.StatusBadRequest, errors.New("missing url or out_dir"))
			return
		}
		maxAttempts := req.MaxAttempts
		if maxAttempts <= 0 && s.Settings != nil {
			if v := s.Settings.GetMaxAttempts(); v > 0 {
				maxAttempts = v
			}
		}
		id, err := s.Queue.CreateJob(r.Context(), req.URL, req.OutDir, req.Name, req.Site, maxAttempts)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		log.Printf("action=add id=%d url=%q out=%q name=%q site=%q max_attempts=%d", id, req.URL, req.OutDir, req.Name, req.Site, maxAttempts)
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

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		writeErr(w, http.StatusInternalServerError, errors.New("settings not initialized"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Settings.Get())
	case http.MethodPost:
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeErr(w, http.StatusRequestEntityTooLarge, errors.New("request body too large"))
				return
			}
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := s.Settings.Update(updates); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := s.Settings.Save(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		log.Printf("action=settings_update")
		writeJSON(w, http.StatusOK, s.Settings.Get())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type browseResponse struct {
	Path   string   `json:"path"`
	Parent string   `json:"parent"`
	Dirs   []string `json:"dirs"`
	IsRoot bool     `json:"is_root"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		// Return root presets if no path specified
		if s.Meta == nil || len(s.Meta.OutDirPresets) == 0 {
			writeJSON(w, http.StatusOK, browseResponse{
				Path:   "",
				Parent: "",
				Dirs:   []string{},
				IsRoot: true,
			})
			return
		}
		writeJSON(w, http.StatusOK, browseResponse{
			Path:   "",
			Parent: "",
			Dirs:   s.Meta.OutDirPresets,
			IsRoot: true,
		})
		return
	}

	// Validate path is under an allowed root
	if !s.isAllowedPath(path) {
		writeErr(w, http.StatusForbidden, errors.New("path not allowed"))
		return
	}

	// Check if path exists and is a directory
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, errors.New("path not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !info.IsDir() {
		writeErr(w, http.StatusBadRequest, errors.New("path is not a directory"))
		return
	}

	// List directories
	entries, err := os.ReadDir(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	dirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)

	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}

	writeJSON(w, http.StatusOK, browseResponse{
		Path:   path,
		Parent: parent,
		Dirs:   dirs,
		IsRoot: s.isRootPreset(path),
	})
}

type mkdirRequest struct {
	Path string `json:"path"`
}

type mkdirResponse struct {
	OK   bool   `json:"ok"`
	Path string `json:"path"`
}

func (s *Server) handleBrowseMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req mkdirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, errors.New("request body too large"))
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}

	// Validate path is under an allowed root
	if !s.isAllowedPath(req.Path) {
		writeErr(w, http.StatusForbidden, errors.New("path not allowed"))
		return
	}

	// Create directory
	if err := os.MkdirAll(req.Path, 0755); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	log.Printf("action=mkdir path=%s", req.Path)
	writeJSON(w, http.StatusOK, mkdirResponse{
		OK:   true,
		Path: req.Path,
	})
}

// isAllowedPath checks if the path is under one of the allowed root directories
func (s *Server) isAllowedPath(path string) bool {
	if s.Meta == nil || len(s.Meta.OutDirPresets) == 0 {
		return false
	}

	cleanPath := filepath.Clean(path)
	for _, root := range s.Meta.OutDirPresets {
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isRootPreset checks if the path is one of the root presets
func (s *Server) isRootPreset(path string) bool {
	if s.Meta == nil || len(s.Meta.OutDirPresets) == 0 {
		return false
	}

	cleanPath := filepath.Clean(path)
	for _, root := range s.Meta.OutDirPresets {
		if cleanPath == filepath.Clean(root) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func withRequestLimit(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
