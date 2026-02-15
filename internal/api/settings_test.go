package api

import (
	"os"
	"testing"
)

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
	s := &Settings{Concurrency: 2, MaxAttempts: 5, AutoDecrypt: false}
	err := s.Update(map[string]interface{}{"max_attempts": 3.1})
	if err == nil {
		t.Fatalf("expected error for non-integer max_attempts")
	}
	if got := err.Error(); got != "max_attempts must be an integer" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestSettingsUpdateRejectsNonBooleanAutoDecrypt(t *testing.T) {
	s := &Settings{Concurrency: 2, MaxAttempts: 5, AutoDecrypt: false}
	err := s.Update(map[string]interface{}{"auto_decrypt": "true"})
	if err == nil {
		t.Fatalf("expected error for non-boolean auto_decrypt")
	}
	if got := err.Error(); got != "auto_decrypt must be a boolean" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestSettingsUpdateAcceptsIntegerValues(t *testing.T) {
	s := &Settings{Concurrency: 2, MaxAttempts: 5, AutoDecrypt: false}
	if err := s.Update(map[string]interface{}{
		"concurrency":  float64(3),
		"max_attempts": float64(7),
		"auto_decrypt": true,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if s.Concurrency != 3 {
		t.Fatalf("expected concurrency=3, got %d", s.Concurrency)
	}
	if s.MaxAttempts != 7 {
		t.Fatalf("expected max_attempts=7, got %d", s.MaxAttempts)
	}
	if !s.AutoDecrypt {
		t.Fatalf("expected auto_decrypt=true, got false")
	}
}

func TestNewSettingsDefaultsEnableAutoDecrypt(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSettings(dir)
	if err != nil {
		t.Fatalf("new settings: %v", err)
	}
	if !s.GetAutoDecrypt() {
		t.Fatalf("expected auto_decrypt=true by default")
	}
	data, err := os.ReadFile(dir + "/settings.json")
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected settings.json to be written")
	}
}
