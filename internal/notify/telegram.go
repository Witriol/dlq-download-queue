package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// telegramAPIBase is the default Bot API base URL; overridden in
	// tests via Notifier.botAPIBase so they can use httptest.Server.
	telegramAPIBase = "https://api.telegram.org"

	// telegramMessageLimit is the Bot API sendMessage text length limit.
	telegramMessageLimit = 4096

	httpTimeout   = 10 * time.Second
	maxRetryAfter = 60 * time.Second
)

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type telegramAPIResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// rateLimitError signals a 429 response; sendMessage retries once after
// waiting retryAfter (capped).
type rateLimitError struct {
	retryAfter time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("telegram: rate limited, retry after %s", e.retryAfter)
}

// sendMessage delivers text, retrying once after a capped wait on HTTP
// 429. The returned error never contains token: *url.Error embeds the
// request URL, which contains it, so every error is redacted before
// it leaves this function.
func (n *Notifier) sendMessage(ctx context.Context, token, chatID, text string) error {
	err := n.postMessage(ctx, token, chatID, text)
	if err == nil {
		return nil
	}

	var rle *rateLimitError
	if !errors.As(err, &rle) {
		return redact(err, token)
	}

	wait := rle.retryAfter
	if wait > maxRetryAfter {
		wait = maxRetryAfter
	}

	select {
	case <-ctx.Done():
		return redact(ctx.Err(), token)
	case <-time.After(wait):
	}

	if err := n.postMessage(ctx, token, chatID, text); err != nil {
		return redact(err, token)
	}
	return nil
}

func (n *Notifier) postMessage(ctx context.Context, token, chatID, text string) error {
	body, err := json.Marshal(sendMessageRequest{ChatID: chatID, Text: text})
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/bot%s/sendMessage", n.botAPIBase, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return sanitizeURLError(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return sanitizeURLError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var parsed telegramAPIResponse
	_ = json.Unmarshal(respBody, &parsed)

	if resp.StatusCode == http.StatusTooManyRequests {
		return &rateLimitError{retryAfter: time.Duration(parsed.Parameters.RetryAfter) * time.Second}
	}
	if parsed.Description != "" {
		return fmt.Errorf("telegram: %s (status %d)", parsed.Description, resp.StatusCode)
	}
	return fmt.Errorf("telegram: unexpected status %d", resp.StatusCode)
}

// sanitizeURLError strips the request URL out of a *url.Error (both a
// url.Parse failure building the request and a transport failure from
// httpClient.Do use this type), since the URL embeds the token: raw in
// the %q-quoted parse-error form, re-escaped (space -> %20) in the
// transport-error form. Only Op and the wrapped error survive.
func sanitizeURLError(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return fmt.Errorf("telegram: %s: %w", uerr.Op, uerr.Err)
	}
	return err
}

// redact is a belt-and-suspenders pass: it removes the raw token from
// whatever text remains after sanitizeURLError has already dropped the URL.
func redact(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "***"))
}
