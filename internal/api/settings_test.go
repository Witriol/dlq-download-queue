package api

import "testing"

func TestSettingsUpdateRejectsNonIntegerConcurrency(t *testing.T) {
	s := &Settings{Concurrency: 2, MaxAttempts: 5}
	err := s.Update(map[string]interface{}{"concurrency": 1.5})
	if err == nil {
		t.Fatalf("expected error for non-integer concurrency")
	}
	if got := err.Error(); got != "concurrency must be an integer" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestSettingsUpdateRejectsNonIntegerMaxAttempts(t *testing.T) {
	s := &Settings{Concurrency: 2, MaxAttempts: 5}
	err := s.Update(map[string]interface{}{"max_attempts": 3.1})
	if err == nil {
		t.Fatalf("expected error for non-integer max_attempts")
	}
	if got := err.Error(); got != "max_attempts must be an integer" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestSettingsUpdateAcceptsIntegerValues(t *testing.T) {
	s := &Settings{Concurrency: 2, MaxAttempts: 5}
	if err := s.Update(map[string]interface{}{
		"concurrency":  float64(3),
		"max_attempts": float64(7),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if s.Concurrency != 3 {
		t.Fatalf("expected concurrency=3, got %d", s.Concurrency)
	}
	if s.MaxAttempts != 7 {
		t.Fatalf("expected max_attempts=7, got %d", s.MaxAttempts)
	}
}
