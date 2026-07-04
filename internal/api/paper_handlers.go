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

// POST /v1/paper/trades/{tradeId}/close
func (s *Server) handleCloseTrade(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tradeId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trade id"})
		return
	}
	trade, err := s.paper.Close(r.Context(), id)
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
