package api

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/Witriol/dlq-download-queue/internal/notify"
)

const defaultConcurrency = 2
const defaultMaxAttempts = 5
const defaultAutoDecrypt = true

// telegramEvents toggles notification delivery per lifecycle event.
type telegramEvents struct {
	Completed     bool `json:"completed"`
	Failed        bool `json:"failed"`
	Retrying      bool `json:"retrying"`
	ExtractFailed bool `json:"extract_failed"`
}

// telegramSettings is the stored Telegram notification configuration.
// BotToken is never included in an API response; see telegramSettingsView.
type telegramSettings struct {
	Enabled           bool           `json:"enabled"`
	BotToken          string         `json:"bot_token"`
	ChatID            string         `json:"chat_id"`
	Events            telegramEvents `json:"events"`
	CompletedTemplate string         `json:"completed_template"`
	FailureTemplate   string         `json:"failure_template"`
}

// defaultTelegramSettings matches the "Settings contract" defaults: disabled,
// completed/failed/extract_failed on, retrying off, templates empty (notify
// applies its own defaults).
func defaultTelegramSettings() telegramSettings {
	return telegramSettings{
		Events: telegramEvents{Completed: true, Failed: true, ExtractFailed: true},
	}
}

// telegramSettingsView is the GET/POST response shape: bot_token replaced by
// bot_token_set so the token itself never leaves the server.
type telegramSettingsView struct {
	Enabled           bool           `json:"enabled"`
	BotTokenSet       bool           `json:"bot_token_set"`
	ChatID            string         `json:"chat_id"`
	Events            telegramEvents `json:"events"`
	CompletedTemplate string         `json:"completed_template"`
	FailureTemplate   string         `json:"failure_template"`
}

// Settings represents runtime application settings
type Settings struct {
	Concurrency int              `json:"concurrency"`
	MaxAttempts int              `json:"max_attempts"`
	AutoDecrypt bool             `json:"auto_decrypt"`
	Telegram    telegramSettings `json:"telegram"`
	mu          sync.RWMutex
	path        string
}

// NewSettings creates a new Settings instance
func NewSettings(stateDir string) (*Settings, error) {
	path := filepath.Join(stateDir, "settings.json")
	s := &Settings{
		Concurrency: defaultConcurrency,
		MaxAttempts: defaultMaxAttempts,
		AutoDecrypt: defaultAutoDecrypt,
		Telegram:    defaultTelegramSettings(),
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
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}

	// Write to a temp file (os.CreateTemp already creates it 0600, so the
	// token is never on disk at a wider mode) and rename over settings.json,
	// rather than writing the token into a pre-existing file before
	// chmod-ing it down from a possibly wider mode.
	tmp, err := os.CreateTemp(dir, ".settings-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp settings file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename settings: %w", err)
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
		"auto_decrypt": s.AutoDecrypt,
		"telegram": telegramSettingsView{
			Enabled:           s.Telegram.Enabled,
			BotTokenSet:       s.Telegram.BotToken != "",
			ChatID:            s.Telegram.ChatID,
			Events:            s.Telegram.Events,
			CompletedTemplate: s.Telegram.CompletedTemplate,
			FailureTemplate:   s.Telegram.FailureTemplate,
		},
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

// GetAutoDecrypt returns whether archive auto-decrypt is enabled.
func (s *Settings) GetAutoDecrypt() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AutoDecrypt
}

// GetTelegram returns the current Telegram settings as a notify.Config.
func (s *Settings) GetTelegram() notify.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return notify.Config{
		Enabled:  s.Telegram.Enabled,
		BotToken: s.Telegram.BotToken,
		ChatID:   s.Telegram.ChatID,
		Events: map[notify.Event]bool{
			notify.EventCompleted:     s.Telegram.Events.Completed,
			notify.EventFailed:        s.Telegram.Events.Failed,
			notify.EventRetrying:      s.Telegram.Events.Retrying,
			notify.EventExtractFailed: s.Telegram.Events.ExtractFailed,
		},
		CompletedTemplate: s.Telegram.CompletedTemplate,
		FailureTemplate:   s.Telegram.FailureTemplate,
	}
}

// Update validates every field into locals first and only assigns them to s
// once all have passed, so a rejected field (e.g. an invalid telegram
// object) never leaves an earlier field in the same call applied.
func (s *Settings) Update(updates map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	concurrency := s.Concurrency
	maxAttempts := s.MaxAttempts
	autoDecrypt := s.AutoDecrypt
	telegram := s.Telegram

	if v, ok := updates["concurrency"]; ok {
		c, ok := v.(float64) // JSON numbers are float64
		if !ok {
			return fmt.Errorf("concurrency must be a number")
		}
		if c != math.Trunc(c) {
			return fmt.Errorf("concurrency must be an integer")
		}
		if c < 1 || c > 10 {
			return fmt.Errorf("concurrency must be between 1 and 10")
		}
		concurrency = int(c)
	}

	if v, ok := updates["max_attempts"]; ok {
		m, ok := v.(float64) // JSON numbers are float64
		if !ok {
			return fmt.Errorf("max_attempts must be a number")
		}
		if m != math.Trunc(m) {
			return fmt.Errorf("max_attempts must be an integer")
		}
		if m < 1 || m > 20 {
			return fmt.Errorf("max_attempts must be between 1 and 20")
		}
		maxAttempts = int(m)
	}

	if v, ok := updates["auto_decrypt"]; ok {
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("auto_decrypt must be a boolean")
		}
		autoDecrypt = b
	}

	if v, ok := updates["telegram"]; ok {
		telegramUpdate, ok := v.(map[string]interface{})
		if !ok {
			return fmt.Errorf("telegram must be an object")
		}
		if err := applyTelegramUpdate(&telegram, telegramUpdate); err != nil {
			return err
		}
	}

	s.Concurrency = concurrency
	s.MaxAttempts = maxAttempts
	s.AutoDecrypt = autoDecrypt
	s.Telegram = telegram

	return nil
}

// applyTelegramUpdate merges a partial telegram update into next, which
// starts as a copy of the stored settings so an absent or empty bot_token
// keeps its stored value. Every field is validated before enabled is
// cross-checked against the resulting token/chat_id, so a rejected update
// leaves next (and therefore the stored settings) untouched.
func applyTelegramUpdate(next *telegramSettings, updates map[string]interface{}) error {
	if v, ok := updates["enabled"]; ok {
		enabled, ok := v.(bool)
		if !ok {
			return fmt.Errorf("telegram.enabled must be a boolean")
		}
		next.Enabled = enabled
	}

	if v, ok := updates["bot_token"]; ok {
		token, ok := v.(string)
		if !ok {
			return fmt.Errorf("telegram.bot_token must be a string")
		}
		token = strings.TrimSpace(token)
		if token != "" {
			if containsWhitespaceOrControl(token) {
				return fmt.Errorf("telegram.bot_token must not contain whitespace or control characters")
			}
			next.BotToken = token
		}
	}

	if v, ok := updates["chat_id"]; ok {
		chatID, ok := v.(string)
		if !ok {
			return fmt.Errorf("telegram.chat_id must be a string")
		}
		next.ChatID = chatID
	}

	if v, ok := updates["completed_template"]; ok {
		tmpl, ok := v.(string)
		if !ok {
			return fmt.Errorf("telegram.completed_template must be a string")
		}
		next.CompletedTemplate = tmpl
	}

	if v, ok := updates["failure_template"]; ok {
		tmpl, ok := v.(string)
		if !ok {
			return fmt.Errorf("telegram.failure_template must be a string")
		}
		next.FailureTemplate = tmpl
	}

	if v, ok := updates["events"]; ok {
		events, ok := v.(map[string]interface{})
		if !ok {
			return fmt.Errorf("telegram.events must be an object")
		}
		if err := applyTelegramEventsUpdate(&next.Events, events); err != nil {
			return err
		}
	}

	if next.Enabled && (next.BotToken == "" || next.ChatID == "") {
		return fmt.Errorf("telegram.enabled requires a bot token and chat_id")
	}

	return nil
}

// containsWhitespaceOrControl reports whether token has any character a
// Telegram bot token cannot legitimately contain; such a token corrupts the
// request line if ever embedded in the API URL (e.g. a "\n" header
// injection) or the log message (redact only matches an exact substring).
func containsWhitespaceOrControl(token string) bool {
	for _, r := range token {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func applyTelegramEventsUpdate(events *telegramEvents, updates map[string]interface{}) error {
	for key, v := range updates {
		enabled, ok := v.(bool)
		if !ok {
			return fmt.Errorf("telegram.events.%s must be a boolean", key)
		}
		switch key {
		case "completed":
			events.Completed = enabled
		case "failed":
			events.Failed = enabled
		case "retrying":
			events.Retrying = enabled
		case "extract_failed":
			events.ExtractFailed = enabled
		default:
			return fmt.Errorf("unknown telegram event %q", key)
		}
	}

	return nil
}
