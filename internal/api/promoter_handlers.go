package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// GET /v1/promoter-trades?days=30 — tracked promoter/director/KMP buys and
// sells from the last N days (default 30, capped at the retention window).
func (s *Server) handleListPromoterTrades(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	list, err := s.promoter.ListRecent(r.Context(), days)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "trades": list})
}

// POST /v1/promoter-trades/scan — trigger an immediate poll. Open to every
// logged-in user; cooldown-guarded inside the service so simultaneous clicks
// from multiple users can't hammer NSE.
func (s *Server) handlePromoterScanNow(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	started, err := s.promoter.ScanNow(r.Context())
	if err != nil {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	if started {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "scanning"})
	}
}

// POST /v1/admin/promoter-trades/{id}/send-alert — force-send the Telegram
// alert for one trade. Admin only.
func (s *Server) handlePromoterSendAlert(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	// chi.URLParam returns the raw (still percent-encoded) segment — it
	// deliberately doesn't decode, so an escaped "/" in a param can't be
	// confused with a path separator. Our ids contain ":" (URL-encoded by
	// the frontend), so we must decode it ourselves before using it as a
	// DB lookup key.
	id, err := url.PathUnescape(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := s.promoter.SendAlert(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "id": id})
}
