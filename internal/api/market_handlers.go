package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/analytics"
	"tradenexus/internal/market"
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
	if !p.HasData {
		return p, nil
	}

	// Anchor the day-change to TODAY vs the last close BEFORE today, instead of
	// ComputeParams' "last candle vs the one before it". In the morning (before
	// today's candle is reconciled into the DB) the last stored candle is
	// yesterday, so ComputeParams would report a stale yesterday-vs-day-before
	// change and set prev_close to the day-before — which also breaks the
	// client-side live-tick recompute. Here prev_close is always yesterday's
	// close and the current price comes from the live tick (0% until the market
	// opens and a tick arrives).
	last := daily[len(daily)-1]
	prevClose := p.PrevClose // ComputeParams: second-to-last close
	current := last.Close    // last stored close
	if !sameISTDate(last.Time, time.Now()) {
		// Last stored candle is a prior day → it IS the previous close, and we
		// have no price for today yet (flat until a live tick lands).
		prevClose = last.Close
		current = last.Close
	}
	if tick, ok := s.live.GetLastTick(inst.Exchange, inst.SymbolToken); ok && tick.Price > 0 {
		current = tick.Price
	}
	p.PrevClose = prevClose
	p.LastClose = current
	p.Price = current
	if prevClose > 0 {
		p.PctChange = (current - prevClose) / prevClose * 100
	} else {
		p.PctChange = 0
	}
	return p, nil
}

// sameISTDate reports whether two instants fall on the same calendar day in IST.
func sameISTDate(a, b time.Time) bool {
	a, b = a.In(market.IST), b.In(market.IST)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
