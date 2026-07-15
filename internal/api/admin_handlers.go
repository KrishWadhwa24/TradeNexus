package api

import (
	"context"
	"net/http"
	"time"

	"tradenexus/internal/market"
)

// parseAdminDate reads the required ?date=YYYY-MM-DD query param (IST).
func parseAdminDate(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	ds := r.URL.Query().Get("date")
	if ds == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date query param required (YYYY-MM-DD)"})
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", ds, market.IST)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date; use YYYY-MM-DD"})
		return time.Time{}, false
	}
	return d, true
}

// GET /v1/admin/candles?date=YYYY-MM-DD — how many instruments have a candle
// stored on that date. Doubles as the missing-candle diagnostic. Admin only.
func (s *Server) handleCandleCountByDate(w http.ResponseWriter, r *http.Request) {
	d, ok := parseAdminDate(w, r)
	if !ok {
		return
	}
	n, err := s.engine.CountCandlesForDate(r.Context(), d)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":           d.Format("2006-01-02"),
		"weekday":        d.Weekday().String(),
		"is_trading_day": s.cal.Cal().IsTradingDay(d),
		"count":          n,
	})
}

// DELETE /v1/admin/candles?date=YYYY-MM-DD — remove every daily candle on that
// date and rebuild affected aggregates. Admin only.
func (s *Server) handleDeleteCandlesByDate(w http.ResponseWriter, r *http.Request) {
	d, ok := parseAdminDate(w, r)
	if !ok {
		return
	}
	deleted, err := s.engine.DeleteCandlesForDate(r.Context(), d)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":    d.Format("2006-01-02"),
		"deleted": deleted,
	})
}

// POST /v1/admin/candles/refetch?date=YYYY-MM-DD — re-fetch that trading day
// from Angel for every tracked instrument (background; one rate-limited call
// per stock). Admin only. Returns 202.
func (s *Server) handleRefetchCandlesByDate(w http.ResponseWriter, r *http.Request) {
	d, ok := parseAdminDate(w, r)
	if !ok {
		return
	}
	if !s.refetchRunning.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "already_running", "message": "a refetch is already in progress",
		})
		return
	}
	go func() {
		defer s.refetchRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		res, err := s.engine.RefetchDate(ctx, d)
		if err != nil {
			s.log.Error().Err(err).Str("date", d.Format("2006-01-02")).Msg("admin refetch failed")
			return
		}
		s.log.Info().Str("date", res.Date).Int("updated", res.Updated).Msg("admin refetch done")
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "started",
		"date":   d.Format("2006-01-02"),
		"message": "refetch started in background",
	})
}
