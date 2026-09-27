package api

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Witriol/dlq-download-queue/internal/notify"
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

func TestNewSettingsWithoutTelegramKeyGetsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/settings.json"
	if err := os.WriteFile(path, []byte(`{"concurrency":3,"max_attempts":6,"auto_decrypt":true}`), 0644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	s, err := NewSettings(dir)
	if err != nil {
		t.Fatalf("new settings: %v", err)
	}
	got := s.GetTelegram()
	if got.Enabled || got.BotToken != "" || got.ChatID != "" {
		t.Fatalf("expected disabled telegram defaults, got %+v", got)
	}
	if !got.Events[notify.EventCompleted] || !got.Events[notify.EventFailed] || !got.Events[notify.EventExtractFailed] {
		t.Fatalf("expected completed/failed/extract_failed on by default: %+v", got.Events)
	}
	if got.Events[notify.EventRetrying] {
		t.Fatalf("expected retrying off by default: %+v", got.Events)
	}
}

func TestSettingsGetNeverIncludesToken(t *testing.T) {
	s := &Settings{Telegram: telegramSettings{BotToken: "shh-secret", ChatID: "123", Enabled: true, Events: telegramEvents{Completed: true}}}
	data, err := json.Marshal(s.Get())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "shh-secret") {
		t.Fatalf("GET response leaked the bot token: %s", data)
	}
	if !strings.Contains(string(data), `"bot_token_set":true`) {
		t.Fatalf("expected bot_token_set=true, got %s", data)
	}
}

func TestSettingsUpdateTelegramPartialKeepsToken(t *testing.T) {
	s := &Settings{Telegram: telegramSettings{BotToken: "stored-token", ChatID: "1", Enabled: true, Events: telegramEvents{Completed: true}}}
	err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"chat_id": "2"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if s.Telegram.BotToken != "stored-token" {
		t.Fatalf("expected stored token to be kept, got %q", s.Telegram.BotToken)
	}
	if s.Telegram.ChatID != "2" {
		t.Fatalf("expected chat_id updated to 2, got %q", s.Telegram.ChatID)
	}
}

func TestSettingsUpdateTelegramEnableWithoutTokenFails(t *testing.T) {
	s := &Settings{Telegram: defaultTelegramSettings()}
	before := s.Telegram
	err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"enabled": true, "chat_id": "123"},
	})
	if err == nil {
		t.Fatalf("expected error enabling telegram without a bot token")
	}
	if s.Telegram != before {
		t.Fatalf("telegram settings changed on a rejected update: got %+v, want %+v", s.Telegram, before)
	}
}

func TestSettingsUpdateTelegramEnableWithoutChatIDFails(t *testing.T) {
	s := &Settings{Telegram: telegramSettings{BotToken: "stored-token"}}
	before := s.Telegram
	err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"enabled": true},
	})
	if err == nil {
		t.Fatalf("expected error enabling telegram without a chat id")
	}
	if s.Telegram != before {
		t.Fatalf("telegram settings changed on a rejected update: got %+v, want %+v", s.Telegram, before)
	}
}

func TestSettingsUpdateTelegramTokenTrimmed(t *testing.T) {
	s := &Settings{Telegram: defaultTelegramSettings()}
	if err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"bot_token": "  tok  "},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if s.Telegram.BotToken != "tok" {
		t.Fatalf("expected trimmed token, got %q", s.Telegram.BotToken)
	}
}

func TestSettingsUpdateTelegramTokenRejectsControlChar(t *testing.T) {
	s := &Settings{Telegram: defaultTelegramSettings()}
	before := s.Telegram
	err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"bot_token": "tok\nX-Evil"},
	})
	if err == nil {
		t.Fatalf("expected error for a bot token containing a control character")
	}
	if s.Telegram != before {
		t.Fatalf("telegram settings changed on a rejected update: got %+v, want %+v", s.Telegram, before)
	}
}

func TestSettingsUpdateTelegramTokenRejectsWhitespaceOnly(t *testing.T) {
	s := &Settings{Telegram: defaultTelegramSettings()}
	before := s.Telegram
	err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"bot_token": "has space"},
	})
	if err == nil {
		t.Fatalf("expected error for a bot token containing whitespace")
	}
	if s.Telegram != before {
		t.Fatalf("telegram settings changed on a rejected update: got %+v, want %+v", s.Telegram, before)
	}
}

func TestSettingsUpdateTelegramUnknownEventKeyFails(t *testing.T) {
	s := &Settings{Telegram: defaultTelegramSettings()}
	err := s.Update(map[string]interface{}{
		"telegram": map[string]interface{}{"events": map[string]interface{}{"bogus": true}},
	})
	if err == nil {
		t.Fatalf("expected error for an unknown event key")
	}
}

func TestSettingsUpdateValidConcurrencyWithInvalidTelegramLeavesConcurrencyUnchanged(t *testing.T) {
	s := &Settings{Concurrency: 2, MaxAttempts: 5, Telegram: defaultTelegramSettings()}
	err := s.Update(map[string]interface{}{
		"concurrency": float64(7),
		"telegram":    map[string]interface{}{"enabled": true}, // no token/chat_id -> invalid
	})
	if err == nil {
		t.Fatalf("expected error for invalid telegram update")
	}
	if s.Concurrency != 2 {
		t.Fatalf("expected concurrency unchanged at 2, got %d", s.Concurrency)
	}
}

// TestSettingsSaveNeverExposesTokenAtWiderMode guards the write/chmod
// ordering on an existing 0644 file: the token must never be readable on
// disk at a mode wider than 0600, even for the instant between the write
// and a trailing chmod. It races a tight stat+read poll against Save; on
// the old write-then-chmod implementation this reliably observes the
// widened window, on a temp-file+rename implementation it cannot.
func TestSettingsSaveNeverExposesTokenAtWiderMode(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/settings.json"
	if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	const token = "secret-token-0600-race"
	s := &Settings{Concurrency: 2, MaxAttempts: 5, Telegram: telegramSettings{BotToken: token}}
	s.path = path

	var wideModeObserved atomic.Bool
	stop := make(chan struct{})
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Read mode and content off the same open fd (not a separate
			// Stat + ReadFile pair) so a concurrent rename can't swap the
			// file out between the two checks and produce a false positive.
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			info, err := f.Stat()
			if err != nil || info.Mode().Perm() == 0600 {
				f.Close()
				continue
			}
			data, _ := io.ReadAll(f)
			f.Close()
			if strings.Contains(string(data), token) {
				wideModeObserved.Store(true)
			}
		}
	}()

	err := s.Save()
	close(stop)
	<-pollDone
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if wideModeObserved.Load() {
		t.Fatalf("token was observable on disk at a mode wider than 0600")
	}
}

func TestSettingsSaveWritesMode0600(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/settings.json"
	// Pre-existing file at 0644, as it would be from before Telegram tokens
	// started landing in settings.json.
	if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	s := &Settings{Concurrency: 2, MaxAttempts: 5}
	s.path = path
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
	}
}
