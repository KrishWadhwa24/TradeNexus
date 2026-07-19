package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/market"
	"tradenexus/internal/signals"
)

// POST /v1/instruments/{id}/scan — run scanners on stored candles, persist signals.
func (s *Server) handleScanInstrument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	res, err := s.engine.ScanStored(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /v1/instruments/{id}/sync-scan?days=1300 — fetch from Angel, then scan.
func (s *Server) handleSyncScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	res, err := s.engine.SyncAndScan(r.Context(), int(id), days)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /v1/signals?instrument_id=&tf=1W&source=weekly&limit=100 — audit browse.
// Non-admins only see signals for instruments on their own watchlists; admins
// see every signal, unscoped.
func (s *Server) handleSignalsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := signals.Filter{
		Timeframe: q.Get("tf"),
		Source:    q.Get("source"),
	}
	if isAdmin, _ := r.Context().Value(isAdminKey).(bool); !isAdmin {
		f.UserID, _ = r.Context().Value(userIDKey).(string)
	}
	if v := q.Get("instrument_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.InstrumentID = &id
		}
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	res, err := s.signals.List(r.Context(), f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(res), "signals": res})
}

// GET /v1/calendar/check?date=2026-01-26 — is it a trading day?
func (s *Server) handleCalendarCheck(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date query param required (YYYY-MM-DD)"})
		return
	}
	d, err := time.ParseInLocation("2006-01-02", dateStr, market.IST)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date; use YYYY-MM-DD"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":           dateStr,
		"is_trading_day": s.cal.Cal().IsTradingDay(d),
		"weekday":        d.Weekday().String(),
	})
}

// POST /v1/admin/reconcile?id=1 — reconcile one instrument, or all if id omitted.
func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	if v := r.URL.Query().Get("id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		res, err := s.engine.Reconcile(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	res, err := s.engine.ReconcileAll(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(res), "results": res})
}

// POST /v1/admin/scan-all — scan all tracked instruments from stored candles.
func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	// Guard: only one scan-all at a time. Repeated clicks are no-ops.
	if !s.scanRunning.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "already_running", "message": "a scan is already in progress",
		})
		return
	}
	// Run in the background with its own context so the HTTP request returns
	// immediately and dispatch (Telegram) is never cancelled by a request timeout.
	go func() {
		defer s.scanRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		res, err := s.engine.ScanAll(ctx)
		if err != nil {
			s.log.Error().Err(err).Msg("scan-all failed")
			return
		}
		s.log.Info().Int("instruments", len(res)).Msg("scan-all done")
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "started", "message": "scan started in background",
	})
}

// POST /v1/admin/cleanup — delete signals older than the retention window now.
func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	n, err := s.engine.Cleanup(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// POST /v1/admin/holidays — add exchange holidays. Body: {"dates":["2026-01-26"]}
func (s *Server) handleAddHolidays(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dates []string `json:"dates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	var dates []time.Time
	for _, ds := range body.Dates {
		d, err := time.ParseInLocation("2006-01-02", ds, market.IST)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date: " + ds})
			return
		}
		dates = append(dates, d)
	}
	n, err := s.cal.AddHolidays(r.Context(), dates)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": n})
}
