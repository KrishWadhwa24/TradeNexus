package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/candles"
	"tradenexus/internal/market"
)

// coverage summarizes what's stored in the DB for one instrument.
type coverage struct {
	InstrumentID       int64  `json:"instrument_id"`
	Symbol             string `json:"symbol"`
	Exchange           string `json:"exchange"`
	DailyCount         int    `json:"daily_candles"`
	FirstDate          string `json:"first_date,omitempty"`
	LastDate           string `json:"last_date,omitempty"`
	WeeklyCount        int    `json:"weekly_candles"`
	MonthlyCount       int    `json:"monthly_candles"`
	TargetDailyBars    int    `json:"target_daily_bars"`    // what we aim to fetch on add
	MissingTradingDays int    `json:"missing_trading_days"` // gaps a reconcile would backfill
	HasData            bool   `json:"has_data"`
}

func (s *Server) buildCoverage(r *http.Request, id int64) (coverage, error) {
	ctx := r.Context()
	c := coverage{InstrumentID: id, TargetDailyBars: candles.RequiredDailyBars}

	inst, err := s.inst.GetByID(ctx, id)
	if err != nil {
		return c, err
	}
	c.Symbol = inst.TradingSymbol
	c.Exchange = inst.Exchange

	set, first, last, ok, err := s.candles.DailyDateSet(ctx, id)
	if err != nil {
		return c, err
	}
	c.HasData = ok
	c.DailyCount = len(set)
	if ok {
		c.FirstDate = first.Format("2006-01-02")
		c.LastDate = last.Format("2006-01-02")
		today := time.Now().In(market.IST)
		c.MissingTradingDays = len(s.cal.Cal().MissingTradingDays(first.AddDate(0, 0, -1), today, set))
	}

	if wk, err := s.candles.GetAggregates(ctx, id, market.TF1W); err == nil {
		c.WeeklyCount = len(wk)
	}
	if mo, err := s.candles.GetAggregates(ctx, id, market.TF1M); err == nil {
		c.MonthlyCount = len(mo)
	}
	return c, nil
}

// GET /v1/instruments/{id}/coverage — storage/coverage for one stock.
func (s *Server) handleCoverage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	cov, err := s.buildCoverage(r, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cov)
}

// GET /v1/users/{uid}/coverage — coverage for every stock in the user's watchlists.
func (s *Server) handleUserCoverage(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	wls, err := s.users.ListWatchlists(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	seen := map[int64]bool{}
	var rows []coverage
	totalDaily := 0
	for _, wl := range wls {
		for _, id := range wl.InstrumentIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			cov, err := s.buildCoverage(r, id)
			if err != nil {
				continue
			}
			rows = append(rows, cov)
			totalDaily += cov.DailyCount
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stocks":              len(rows),
		"total_daily_candles": totalDaily,
		"coverage":            rows,
	})
}
