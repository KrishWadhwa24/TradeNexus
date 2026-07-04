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
	"time"
)

// Telegram is a minimal Telegram Bot API client. BaseURL is overridable for tests.
type Telegram struct {
	http    *http.Client
	baseURL string
}

// NewTelegram builds the client. Pass "" for the production base URL.
func NewTelegram(baseURL string) *Telegram {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &Telegram{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: baseURL,
	}
}

type sendMessageReq struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type sendMessageResp struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// Send delivers a text message via a user's bot token to their chat.
func (t *Telegram) Send(ctx context.Context, botToken, chatID, text string) error {
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram: missing bot token or chat id")
	}
	body, _ := json.Marshal(sendMessageReq{ChatID: chatID, Text: text})
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, botToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var r sendMessageResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("telegram decode (status %d): %w", resp.StatusCode, err)
	}
	if !r.OK {
		return fmt.Errorf("telegram send failed: %s", r.Description)
	}
	return nil
}
