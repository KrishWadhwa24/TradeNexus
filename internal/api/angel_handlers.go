package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

// POST /v1/angel/login — authenticate with Angel (TOTP+JWT).
func (s *Server) handleAngelLogin(w http.ResponseWriter, r *http.Request) {
	if err := s.angel.Login(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.angel.TokenStatus())
}

// GET /v1/angel/status — non-secret token snapshot.
func (s *Server) handleAngelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.angel.TokenStatus())
}

// POST /v1/angel/scripmaster/sync — download NSE-EQ scrips into instruments.
func (s *Server) handleScripMasterSync(w http.ResponseWriter, r *http.Request) {
	scrips, err := s.angel.FetchScripMaster(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	items := make([]instruments.Instrument, 0, len(scrips))
	byExchange := map[string]int{}
	for _, sc := range scrips {
		lot, _ := strconv.Atoi(sc.LotSize)
		if lot == 0 {
			lot = 1
		}
		byExchange[sc.ExchSeg]++
		items = append(items, instruments.Instrument{
			SymbolToken:   sc.Token,
			Exchange:      sc.ExchSeg,
			TradingSymbol: sc.Symbol,
			Name:          sc.Name,
			LotSize:       lot,
		})
	}
	n, err := s.inst.BulkUpsert(r.Context(), items)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fetched": len(scrips), "upserted": n, "by_exchange": byExchange,
	})
}

// POST /v1/angel/historical — raw daily-candle passthrough for testing.
// Body: {"exchange":"NSE","symbol_token":"3045","from":"2024-01-01","to":"2024-03-01"}
// from/to optional (default: last 30 days).
func (s *Server) handleAngelHistorical(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Exchange    string `json:"exchange"`
		SymbolToken string `json:"symbol_token"`
		From        string `json:"from"`
		To          string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	if req.Exchange == "" || req.SymbolToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exchange and symbol_token are required"})
		return
	}

	to := time.Now().In(market.IST)
	from := to.AddDate(0, 0, -30)
	const layout = "2006-01-02"
	if req.From != "" {
		if t, err := time.ParseInLocation(layout, req.From, market.IST); err == nil {
			from = t
		}
	}
	if req.To != "" {
		if t, err := time.ParseInLocation(layout, req.To, market.IST); err == nil {
			to = t
		}
	}

	cs, err := s.angel.GetDailyCandles(r.Context(), req.Exchange, req.SymbolToken, from, to)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(cs), "candles": cs})
}

// GET /v1/instruments/{id}
func (s *Server) handleInstrumentGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	it, err := s.inst.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, instruments.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "instrument not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// GET /v1/instruments/search?q=REL&limit=10
func (s *Server) handleInstrumentSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := s.inst.Search(r.Context(), q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(res), "instruments": res})
}
