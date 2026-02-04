package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const defaultConcurrency = 2
const defaultMaxAttempts = 5

// Settings represents runtime application settings
type Settings struct {
	Concurrency int `json:"concurrency"`
	MaxAttempts int `json:"max_attempts"`
	mu          sync.RWMutex
	path        string
}

// NewSettings creates a new Settings instance
func NewSettings(stateDir string) (*Settings, error) {
	path := filepath.Join(stateDir, "settings.json")
	s := &Settings{
		Concurrency: defaultConcurrency,
		MaxAttempts: defaultMaxAttempts,
		path:        path,
	}

	// Try to load from file, fall back to defaults if it doesn't exist
	if err := s.load(); err != nil {
		if os.IsNotExist(err) {
			if err := s.Save(); err != nil {
				return nil, fmt.Errorf("save default settings: %w", err)
			}
		} else {
			return nil, fmt.Errorf("load settings: %w", err)
		}
	}

	return s, nil
}

// load reads settings from the JSON file
func (s *Settings) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, s)
}

// Save writes settings to the JSON file
func (s *Settings) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	return nil
}

// Get returns a copy of the current settings
func (s *Settings) Get() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"concurrency":  s.Concurrency,
		"max_attempts": s.MaxAttempts,
	}
}

// GetConcurrency returns the current concurrency setting (thread-safe)
func (s *Settings) GetConcurrency() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Concurrency
}

// GetMaxAttempts returns the current max attempts setting (thread-safe)
func (s *Settings) GetMaxAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MaxAttempts
}

// Update updates settings with the provided values and saves to disk
func (s *Settings) Update(updates map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate and apply updates
	if v, ok := updates["concurrency"]; ok {
		concurrency, ok := v.(float64) // JSON numbers are float64
		if !ok {
			return fmt.Errorf("concurrency must be a number")
		}
		if concurrency < 1 || concurrency > 10 {
			return fmt.Errorf("concurrency must be between 1 and 10")
		}
		s.Concurrency = int(concurrency)
	}

	if v, ok := updates["max_attempts"]; ok {
		maxAttempts, ok := v.(float64) // JSON numbers are float64
		if !ok {
			return fmt.Errorf("max_attempts must be a number")
		}
		if maxAttempts < 1 || maxAttempts > 20 {
			return fmt.Errorf("max_attempts must be between 1 and 20")
		}
		s.MaxAttempts = int(maxAttempts)
	}

	return nil
}
