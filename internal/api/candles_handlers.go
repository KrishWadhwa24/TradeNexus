package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/candles"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

// POST /v1/instruments/{id}/candles/sync?days=1300
// Fetches daily history from Angel, stores it, and rebuilds weekly/monthly.
func (s *Server) handleCandleSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}

	days := candles.RequiredDailyBars
	if q := r.URL.Query().Get("days"); q != "" {
		if d, err := strconv.Atoi(q); err == nil && d > 0 {
			days = d
		}
	}
	if days > 2000 { // Angel single-request cap for ONE_DAY
		days = 2000
	}

	inst, err := s.inst.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, instruments.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "instrument not found; sync scrip master first"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Angel returns only trading days, so widen the calendar span to land ~days bars.
	to := time.Now().In(market.IST)
	calendarSpan := days*7/5 + 10
	from := to.AddDate(0, 0, -calendarSpan)

	fetched, err := s.angel.GetDailyCandles(r.Context(), inst.Exchange, inst.SymbolToken, from, to)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	stored, err := s.candles.UpsertDaily(r.Context(), id, fetched)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	weekly, monthly, err := s.candles.RebuildAggregates(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instrument_id":  id,
		"trading_symbol": inst.TradingSymbol,
		"daily_fetched":  len(fetched),
		"daily_stored":   stored,
		"weekly_candles": weekly,
		"monthly_candles": monthly,
	})
}

// GET /v1/instruments/{id}/candles?tf=1D|1W|1M&limit=50
func (s *Server) handleCandleGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	tf := r.URL.Query().Get("tf")
	if tf == "" {
		tf = market.TF1D
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	switch tf {
	case market.TF1D:
		cs, err := s.candles.GetDaily(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		cs = tailDaily(cs, limit)
		writeJSON(w, http.StatusOK, map[string]any{"timeframe": tf, "count": len(cs), "candles": cs})
	case market.TF1W, market.TF1M:
		cs, err := s.candles.GetAggregates(r.Context(), id, tf)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		cs = tailAgg(cs, limit)
		writeJSON(w, http.StatusOK, map[string]any{"timeframe": tf, "count": len(cs), "candles": cs})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tf must be 1D, 1W, or 1M"})
	}
}

func tailDaily(cs []market.Candle, limit int) []market.Candle {
	if limit > 0 && len(cs) > limit {
		return cs[len(cs)-limit:]
	}
	return cs
}

func tailAgg(cs []market.AggCandle, limit int) []market.AggCandle {
	if limit > 0 && len(cs) > limit {
		return cs[len(cs)-limit:]
	}
	return cs
}
