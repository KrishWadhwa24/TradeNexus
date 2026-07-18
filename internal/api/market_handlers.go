package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/analytics"
)

// GET /v1/market/trending?limit=20 — stocks with the highest daily % gain.
func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	movers, err := s.analytics.TopMovers(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(movers), "trending": movers})
}

// GET /v1/public/market-preview?limit=8 — a small set of trending stocks with
// live-ish params for the pre-login landing page. PUBLIC (no auth).
func (s *Server) handlePublicPreview(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 12 {
		limit = 8
	}
	if s.analytics == nil {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "rows": []analytics.Params{}})
		return
	}
	movers, err := s.analytics.TopMovers(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]analytics.Params, 0, len(movers))
	for _, m := range movers {
		p, perr := s.instrumentParams(r, m.InstrumentID)
		if perr != nil || !p.HasData {
			continue
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "rows": out})
}

// GET /v1/instruments/{id}/params — latest indicators + live price for one stock.
func (s *Server) handleInstrumentParams(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	p, err := s.instrumentParams(r, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// GET /v1/users/{uid}/dashboard — params (with live price) for every stock in
// the user's watchlists. Backs the analytics dashboard.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	wls, err := s.users.ListWatchlists(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	seen := map[int64]bool{}
	var out []analytics.Params
	for _, wl := range wls {
		for _, id := range wl.InstrumentIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			p, err := s.instrumentParams(r, id)
			if err != nil {
				continue
			}
			// Skip stocks with no stored candles (e.g. a failed history fetch) —
			// they'd otherwise show as all-zero rows in the dashboard.
			if !p.HasData {
				continue
			}
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "rows": out})
}

// instrumentParams computes params for one instrument, using live LTP when
// available and falling back to the last close.
func (s *Server) instrumentParams(r *http.Request, id int64) (analytics.Params, error) {
	inst, err := s.inst.GetByID(r.Context(), id)
	if err != nil {
		return analytics.Params{}, err
	}
	daily, err := s.candles.GetDaily(r.Context(), id)
	if err != nil {
		return analytics.Params{}, err
	}
	p := analytics.ComputeParams(daily)
	p.InstrumentID = id
	p.Symbol = inst.TradingSymbol
	// Best-effort live price.
	if tick, ok := s.live.GetLastTick(inst.Exchange, inst.SymbolToken); ok {
		p.Price = tick.Price
	}
	return p, nil
}
