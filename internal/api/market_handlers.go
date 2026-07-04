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
	if ltp, err := s.angel.GetLTP(r.Context(), inst.Exchange, inst.TradingSymbol, inst.SymbolToken); err == nil && ltp > 0 {
		p.Price = ltp
	}
	return p, nil
}
