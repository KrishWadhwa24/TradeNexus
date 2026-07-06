package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/notify"
)

// GET /v1/signals/{id}/recipients — preview who would receive this signal
// (watchlist ∩ enabled scanner ∩ enabled telegram). Does not send.
func (s *Server) handleSignalRecipients(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications disabled (NOTIFY_ENABLED=false)"})
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid signal id"})
		return
	}
	sig, err := s.signals.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	recips, err := s.notifier.Recipients(r.Context(), sig.InstrumentID, notify.ScannerKeys(sig))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ids := make([]string, 0, len(recips))
	for _, rr := range recips {
		ids = append(ids, rr.UserID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"signal_id": id, "recipient_count": len(ids), "user_ids": ids})
}

// POST /v1/telegram/test — send a connectivity-check message. Bypasses the
// signal pipeline (no window, no dedup). Body optional:
//
//	{}                              -> sends to the env default/safety-net chat
//	{"user_id":"<uuid>"}           -> sends via that user's saved bot/chat
//	{"bot_token":"...","chat_id":"..."} -> sends to an ad-hoc bot/chat
func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications disabled (NOTIFY_ENABLED=false)"})
		return
	}
	var body struct {
		UserID   string `json:"user_id"`
		BotToken string `json:"bot_token"`
		ChatID   string `json:"chat_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var err error
	switch {
	case body.BotToken != "" && body.ChatID != "":
		err = s.notifier.SendTest(r.Context(), body.BotToken, body.ChatID)
	case body.UserID != "":
		cfg, e := s.users.GetTelegram(r.Context(), body.UserID)
		if e != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user has no telegram config"})
			return
		}
		err = s.notifier.SendTest(r.Context(), cfg.BotToken, cfg.ChatID)
	default:
		err = s.notifier.TestDefault(r.Context())
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// POST /v1/admin/dispatch?signal_id=1 — manually dispatch a stored signal
// (applies the 7-day window + dedup). Useful for testing the fan-out.
func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications disabled (NOTIFY_ENABLED=false)"})
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("signal_id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signal_id query param required"})
		return
	}
	sig, err := s.signals.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	res, err := s.notifier.Dispatch(r.Context(), sig)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
