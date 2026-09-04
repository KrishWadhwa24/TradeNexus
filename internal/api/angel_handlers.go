package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/angel"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

// POST /v1/angel/login — authenticate with Angel (TOTP+JWT).
func (s *Server) handleAngelLogin(w http.ResponseWriter, r *http.Request) {
	if err := s.angel.ForceLogin(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.angel.TokenStatus())
}

// GET /v1/angel/status — non-secret token snapshot.
func (s *Server) handleAngelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.angel.TokenStatus())
}

// POST /v1/angel/scripmaster/sync — download NSE/BSE cash equities into instruments.
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

// POST /v1/admin/angel/derivatives/sync — pulls near-dated NIFTY/BANKNIFTY/
// FINNIFTY (NFO) and SENSEX/BANKEX (BFO) option chains, plus the Nifty 50 and
// SENSEX index-spot instruments, into `instruments`, and deactivates any
// option past expiry. Same logic the weekly refresh cron runs — safe to
// re-run manually any time (see instruments.SyncDerivatives).
func (s *Server) handleDerivativesSync(w http.ResponseWriter, r *http.Request) {
	res, err := instruments.SyncDerivatives(r.Context(), s.angel, s.inst)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fetched": res.Fetched, "upserted": res.Upserted,
		"options": res.Options, "index_spots": res.IndexSpots, "futures": res.Futures,
		"deactivated": res.Deactivated,
	})
}

// POST /v1/angel/historical — raw candle passthrough for testing.
// Body: {"exchange":"NSE","symbol_token":"3045","from":"2024-01-01","to":"2024-03-01","interval":"ONE_MINUTE"}
// from/to optional (default: last 30 days); interval optional (default: ONE_DAY,
// unchanged from before this field existed — this is a manual/Postman testing
// endpoint with no real caller, so adding it is zero-risk to anything else).
func (s *Server) handleAngelHistorical(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Exchange    string `json:"exchange"`
		SymbolToken string `json:"symbol_token"`
		From        string `json:"from"`
		To          string `json:"to"`
		Interval    string `json:"interval"`
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

	var cs []market.Candle
	var err error
	if req.Interval == "" || req.Interval == angel.IntervalOneDay {
		cs, err = s.angel.GetDailyCandles(r.Context(), req.Exchange, req.SymbolToken, from, to)
	} else {
		cs, err = s.angel.GetIntradayCandles(r.Context(), req.Exchange, req.SymbolToken, req.Interval, from, to)
	}
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
