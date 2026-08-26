// Package notify handles signal distribution: formatting messages, deciding
// recipients (fan-out), enforcing the send window + dedup, and delivering via
// Telegram.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Telegram is a minimal Telegram Bot API client. BaseURL is overridable for tests.
type Telegram struct {
	http    *http.Client
	baseURL string

	// Per-chat pacing. Telegram caps messages to a single chat (~20/min for a
	// group), so a burst of alerts to one group gets 429'd. We space sends to
	// the same chat by minInterval, and separately honour any retry_after the
	// API hands back. Keyed by chat id so fan-out to many DIFFERENT chats isn't
	// serialized behind one busy group.
	mu          sync.Mutex
	lastByChat  map[string]time.Time
	minInterval time.Duration
}

// NewTelegram builds the client. Pass "" for the production base URL.
func NewTelegram(baseURL string) *Telegram {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &Telegram{
		http:       &http.Client{Timeout: 15 * time.Second},
		baseURL:    baseURL,
		lastByChat: make(map[string]time.Time),
		// Telegram allows ~20 messages/min to one group chat. 3s spacing keeps a
		// burst of alerts (all to the same forum group) just under that. Pacing
		// is per-chat, so fan-out to many different user chats isn't throttled.
		minInterval: 3 * time.Second,
	}
}

type sendMessageReq struct {
	ChatID          string `json:"chat_id"`
	Text            string `json:"text"`
	MessageThreadID int    `json:"message_thread_id,omitempty"`
}

type sendMessageResp struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// maxSendAttempts bounds retries when Telegram rate-limits (HTTP 429). Each
// retry waits the API-provided retry_after (capped by retryAfterCap).
const (
	maxSendAttempts = 4
	retryAfterCap   = 60 * time.Second
)

// Send delivers a text message via a user's bot token to their chat. threadID
// targets a specific topic in a forum supergroup; pass 0 for the chat's
// General topic (or for chats that aren't forums at all, e.g. a user's own
// private bot chat). Sends to the same chat are paced, and a 429 is retried
// after the server-provided delay, so a burst of alerts isn't dropped.
func (t *Telegram) Send(ctx context.Context, botToken, chatID string, threadID int, text string) error {
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram: missing bot token or chat id")
	}
	body, _ := json.Marshal(sendMessageReq{ChatID: chatID, Text: text, MessageThreadID: threadID})
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, botToken)

	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if err := t.pace(ctx, chatID); err != nil {
			return err // ctx cancelled while waiting for the pacing slot
		}
		r, err := t.doSend(ctx, url, body)
		if err != nil {
			return err // network/transport error — not worth retrying here
		}
		if r.OK {
			return nil
		}
		if r.Parameters.RetryAfter > 0 && attempt < maxSendAttempts {
			lastErr = fmt.Errorf("telegram rate limited: retry after %ds", r.Parameters.RetryAfter)
			wait := time.Duration(r.Parameters.RetryAfter) * time.Second
			if wait > retryAfterCap {
				wait = retryAfterCap
			}
			if !sleep(ctx, wait) {
				return ctx.Err()
			}
			continue
		}
		return fmt.Errorf("telegram send failed: %s", r.Description)
	}
	return lastErr
}

func (t *Telegram) doSend(ctx context.Context, url string, body []byte) (sendMessageResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return sendMessageResp{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return sendMessageResp{}, fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r sendMessageResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return sendMessageResp{}, fmt.Errorf("telegram decode (status %d): %w", resp.StatusCode, err)
	}
	return r, nil
}

// pace reserves the next send slot for a chat, spacing consecutive sends to the
// same chat by minInterval, then waits for it (respecting ctx).
func (t *Telegram) pace(ctx context.Context, chatID string) error {
	t.mu.Lock()
	now := time.Now()
	next := t.lastByChat[chatID].Add(t.minInterval)
	if next.Before(now) {
		next = now
	}
	t.lastByChat[chatID] = next
	t.mu.Unlock()

	if d := time.Until(next); d > 0 {
		if !sleep(ctx, d) {
			return ctx.Err()
		}
	}
	return nil
}

// sleep waits for d or until ctx is done; returns false if ctx was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
