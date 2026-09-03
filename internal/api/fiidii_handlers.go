package api

import (
	"net/http"
	"strconv"

	"tradenexus/internal/fiidii"
)

// GET /v1/insights/fii-dii — the most recently fetched DII/FII buy/sell/net
// snapshot. No history is kept, so this is always "latest or nothing".
func (s *Server) handleFiiDiiLatest(w http.ResponseWriter, r *http.Request) {
	if s.fiidii == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "fii/dii tracking disabled"})
		return
	}
	snap, ok := s.fiidii.Latest()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":  true,
		"date":       snap.Date,
		"dii":        snap.DII,
		"fii":        snap.FII,
		"fetched_at": snap.FetchedAt,
	})
}

// GET /v1/insights/fii-dii/history?period=weekly|monthly&count=12 — DII/FII
// flow summed per week or month, oldest first, for trend charting.
func (s *Server) handleFiiDiiHistory(w http.ResponseWriter, r *http.Request) {
	if s.fiidii == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "fii/dii tracking disabled"})
		return
	}
	q := r.URL.Query()
	count, _ := strconv.Atoi(q.Get("count"))
	var (
		points []fiidii.PeriodFlow
		err    error
	)
	if q.Get("period") == "monthly" {
		points, err = s.fiidii.Monthly(r.Context(), count)
	} else {
		points, err = s.fiidii.Weekly(r.Context(), count)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(points), "points": points})
}

// POST /v1/admin/fii-dii/send-alert — force-send the cached snapshot to
// Telegram now, bypassing the reconcile-done gate (admin manual override).
func (s *Server) handleFiiDiiSendAlert(w http.ResponseWriter, r *http.Request) {
	if s.fiidii == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "fii/dii tracking disabled"})
		return
	}
	if err := s.fiidii.SendAlert(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}
