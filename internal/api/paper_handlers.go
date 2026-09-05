package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// PUT /v1/users/{uid}/paper/capital  {"capital":100000}
func (s *Server) handleSetCapital(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Capital float64 `json:"capital"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	acct, err := s.paper.SetCapital(r.Context(), chi.URLParam(r, "uid"), body.Capital)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

// GET /v1/users/{uid}/paper/account
func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	acct, err := s.paper.GetAccount(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

// POST /v1/users/{uid}/paper/trades  {"signal_id":1,"quantity":10}
func (s *Server) handleBuy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SignalID int64 `json:"signal_id"`
		Quantity int   `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SignalID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signal_id and quantity required"})
		return
	}
	trade, err := s.paper.Buy(r.Context(), chi.URLParam(r, "uid"), body.SignalID, body.Quantity, "web")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, trade)
}

// POST /v1/users/{uid}/paper/trades/open
// {"instrument_id":1461,"quantity":10,"side":"BUY","product_type":"DELIVERY"}
// The generalized "search any stock and trade it" entry point — sits
// alongside (not replacing) handleBuy's signal-gated flow above, which
// Scanner.jsx keeps using unchanged.
func (s *Server) handleOpenPosition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstrumentID int64  `json:"instrument_id"`
		Quantity     int    `json:"quantity"`
		Side         string `json:"side"`
		ProductType  string `json:"product_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InstrumentID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instrument_id and quantity required"})
		return
	}
	trade, err := s.paper.OpenPosition(r.Context(), chi.URLParam(r, "uid"), body.InstrumentID, body.Quantity, body.Side, body.ProductType, nil, "web")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, trade)
}

// POST /v1/paper/trades/{tradeId}/convert — upgrade an OPEN intraday long
// to delivery by paying the remaining margin.
func (s *Server) handleConvertToDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tradeId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trade id"})
		return
	}
	trade, err := s.paper.ConvertToDelivery(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, trade)
}

// POST /v1/paper/trades/{tradeId}/cancel — cancel a not-yet-filled SCHEDULED
// buy, or a pending close on an OPEN position.
func (s *Server) handleCancelScheduled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tradeId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trade id"})
		return
	}
	trade, err := s.paper.CancelPending(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, trade)
}

// POST /v1/paper/trades/{tradeId}/close  {"quantity":5}
// quantity is optional — omitted, zero, or >= the held quantity closes the
// entire position; a smaller quantity sells down part of it.
func (s *Server) handleCloseTrade(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tradeId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trade id"})
		return
	}
	var body struct {
		Quantity int `json:"quantity"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	trade, err := s.paper.ClosePartial(r.Context(), id, body.Quantity)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, trade)
}

// GET /v1/users/{uid}/paper/trades
func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	trades, err := s.paper.Trades(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(trades), "trades": trades})
}

// GET /v1/users/{uid}/paper/summary
func (s *Server) handlePaperSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.paper.Summary(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

// PUT /v1/users/{uid}/paper/algo-capital — mirrors handleSetCapital, scoped
// to algo_cash_balance instead of cash_balance. Not admin-gated, same as
// handleSetCapital: a user setting their own algo capital is no different
// from setting their own regular capital.
func (s *Server) handleSetAlgoCapital(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Capital float64 `json:"capital"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	acct, err := s.paper.SetAlgoCapital(r.Context(), chi.URLParam(r, "uid"), body.Capital)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

// PUT /v1/users/{uid}/paper/algo-enabled — the frontend on/off switch for
// this account's auto-trading (replaces the old single-account
// OPTIONS_ALGO_USER_EMAIL env var). Not admin-gated, same reasoning as
// handleSetAlgoCapital: a user switching their own auto-trading on/off is
// no different from setting their own capital.
func (s *Server) handleSetAlgoEnabled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	acct, err := s.paper.SetAlgoEnabled(r.Context(), chi.URLParam(r, "uid"), body.Enabled)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

// GET /v1/users/{uid}/paper/algo-stats — win rate/expectancy/profit-factor
// breakdown over CLOSED algo trades. Always computed, even below the
// script's 30-trade "ready to tune" threshold — ReadyForTuning tells the
// frontend whether to treat the numbers as meaningful yet.
func (s *Server) handleAlgoStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.optionsAlgoSvc.Stats(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// GET /v1/users/{uid}/paper/algo-summary — the same rollup as
// handlePaperSummary but for options-algo trades only, against
// algo_cash_balance — powers the Algo Trades section of the Options page.
func (s *Server) handleAlgoSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.paper.AlgoSummary(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sum)
}
