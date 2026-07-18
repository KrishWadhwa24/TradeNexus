package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// GET /v1/ipos — open + upcoming IPOs with GMP.
func (s *Server) handleListIPOs(w http.ResponseWriter, r *http.Request) {
	if s.ipo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ipo tracking disabled"})
		return
	}
	list, err := s.ipo.ListActive(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "ipos": list})
}

// POST /v1/admin/ipos/refresh — poll the IPO feed immediately. Admin only.
func (s *Server) handleRefreshIPOs(w http.ResponseWriter, r *http.Request) {
	if s.ipo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ipo tracking disabled"})
		return
	}
	// Own context so a slow upstream fetch isn't cut by the request timeout.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.ipo.RefreshNow(ctx); err != nil {
			s.log.Error().Err(err).Msg("admin ipo refresh failed")
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "refreshing"})
}

// POST /v1/admin/ipos/{id}/apply — push an "Apply (said by admin)" IPO signal.
func (s *Server) handleIPOAdminApply(w http.ResponseWriter, r *http.Request) {
	if s.ipo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ipo tracking disabled"})
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ipo id"})
		return
	}
	if err := s.ipo.AdminApply(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "ipo_id": id})
}

// POST /v1/admin/ipos/{id}/clear-signal — remove the on-site signal badge for
// all users (does not touch Telegram). Admin only.
func (s *Server) handleIPOClearSignal(w http.ResponseWriter, r *http.Request) {
	if s.ipo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ipo tracking disabled"})
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ipo id"})
		return
	}
	if err := s.ipo.ClearSignal(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "cleared", "ipo_id": id})
}
