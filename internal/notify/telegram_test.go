package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// On a 429 with retry_after, Send should wait and retry rather than drop the
// message (the bug that lost deal alerts during a burst).
func TestSend_RetriesOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = io.WriteString(w, `{"ok":false,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	tg := NewTelegram(srv.URL)
	if err := tg.Send(context.Background(), "tok", "chat", 0, "hi"); err != nil {
		t.Fatalf("Send should succeed after retry, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 attempts (429 then ok), got %d", got)
	}
}

// A non-rate-limit failure should not be retried.
func TestSend_NoRetryOnPlainError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"ok":false,"description":"chat not found"}`)
	}))
	defer srv.Close()

	tg := NewTelegram(srv.URL)
	if err := tg.Send(context.Background(), "tok", "chat", 0, "hi"); err == nil {
		t.Fatal("expected error for chat-not-found")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("plain error must not retry, got %d attempts", got)
	}
}

// The forum topic id must be forwarded as message_thread_id.
func TestSend_ForwardsThreadID(t *testing.T) {
	var gotThread int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req sendMessageReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotThread = req.MessageThreadID
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	tg := NewTelegram(srv.URL)
	if err := tg.Send(context.Background(), "tok", "chat", 55, "hi"); err != nil {
		t.Fatal(err)
	}
	if gotThread != 55 {
		t.Errorf("expected message_thread_id=55, got %d", gotThread)
	}
}
