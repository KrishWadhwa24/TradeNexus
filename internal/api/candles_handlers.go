package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/candles"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

// errUpstream wraps an Angel-fetch failure specifically, so callers can tell
// it apart from a local (Postgres) error — the former is a 502, the latter a
// 500.
var errUpstream = errors.New("upstream fetch failed")

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

	res, err := s.syncCandles(r.Context(), id, days)
	if err != nil {
		switch {
		case errors.Is(err, instruments.ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "instrument not found; sync scrip master first"})
		case errors.Is(err, errUpstream):
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instrument_id":   id,
		"trading_symbol":  res.symbol,
		"daily_fetched":   res.fetched,
		"daily_stored":    res.stored,
		"weekly_candles":  res.weekly,
		"monthly_candles": res.monthly,
	})
}

type candleSyncResult struct {
	symbol          string
	fetched, stored int
	weekly, monthly int
}

// syncCandles fetches daily history from Angel for one instrument, stores it,
// and rebuilds weekly/monthly aggregates. Shared by handleCandleSync (the
// direct admin/watchlist action) and handleAddFeaturedStock (so a newly
// featured stock isn't left with no price data — see AddFeatured's caller).
func (s *Server) syncCandles(ctx context.Context, id int64, days int) (candleSyncResult, error) {
	if days <= 0 {
		days = candles.RequiredDailyBars
	}
	if days > 2000 { // Angel single-request cap for ONE_DAY
		days = 2000
	}

	inst, err := s.inst.GetByID(ctx, id)
	if err != nil {
		return candleSyncResult{}, err
	}

	// Angel returns only trading days, so widen the calendar span to land ~days bars.
	to := time.Now().In(market.IST)
	calendarSpan := days*7/5 + 10
	from := to.AddDate(0, 0, -calendarSpan)

	fetched, err := s.angel.GetDailyCandles(ctx, inst.Exchange, inst.SymbolToken, from, to)
	if err != nil {
		return candleSyncResult{}, fmt.Errorf("%w: %v", errUpstream, err)
	}
	stored, err := s.candles.UpsertDaily(ctx, id, fetched)
	if err != nil {
		return candleSyncResult{}, err
	}
	weekly, monthly, err := s.candles.RebuildAggregates(ctx, id)
	if err != nil {
		return candleSyncResult{}, err
	}
	return candleSyncResult{
		symbol: inst.TradingSymbol, fetched: len(fetched), stored: stored, weekly: weekly, monthly: monthly,
	}, nil
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
