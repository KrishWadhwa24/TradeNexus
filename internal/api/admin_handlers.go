package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/optionsalgo"
)

// GET /v1/admin/angel/quote-full?exchange=NFO&tokens=42635,42636 — manual
// live-verification for the new GetOptionQuoteFull integration (bid/ask/
// volume/OI). No real callers — same "manual testing, admin-only" purpose as
// handleAngelHistorical. Admin only.
func (s *Server) handleAngelQuoteFullTest(w http.ResponseWriter, r *http.Request) {
	exchange := r.URL.Query().Get("exchange")
	tokensParam := r.URL.Query().Get("tokens")
	if exchange == "" || tokensParam == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exchange and tokens query params required"})
		return
	}
	tokens := strings.Split(tokensParam, ",")
	quotes, err := s.angel.GetOptionQuoteFull(r.Context(), exchange, tokens)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quotes": quotes})
}

// GET /v1/admin/angel/option-greeks?name=NIFTY&expiry=08SEP2026 — manual
// live-verification for the new GetOptionGreeks integration. Admin only.
func (s *Server) handleAngelOptionGreeksTest(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	expiry := r.URL.Query().Get("expiry")
	if name == "" || expiry == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and expiry query params required"})
		return
	}
	greeks, err := s.angel.GetOptionGreeks(r.Context(), name, expiry)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"greeks": greeks})
}

// GET /v1/admin/optionsalgo/direction — live-verification for Phase 1's
// market-direction engine (internal/optionsalgo/direction.go): runs
// EvaluateDirection against real stored candles and returns both the
// classification and the raw indicator values that produced it. Read-only,
// no trade is placed or affected. Admin only.
func (s *Server) handleOptionsAlgoDirection(w http.ResponseWriter, r *http.Request) {
	result, inputs, err := s.optionsAlgoSvc.EvaluateDirection(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"direction": result.Direction,
		"reason":    result.Reason,
		"inputs":    inputs,
	})
}

// GET /v1/admin/optionsalgo/option-chain — live-verification for Phase 2's
// option chain + strike selection (internal/optionsalgo/chain.go,
// chain_live.go): builds the real ATM+/-5 NIFTY chain (live bid/ask/volume/
// OI/Greeks), runs the direction engine to decide which side to look at, and
// applies delta+liquidity selection. Read-only, no trade is placed or
// affected. Admin only.
func (s *Server) handleOptionsAlgoOptionChain(w http.ResponseWriter, r *http.Request) {
	direction, inputs, err := s.optionsAlgoSvc.EvaluateDirection(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "direction: " + err.Error()})
		return
	}
	chain, err := s.optionsAlgoSvc.BuildOptionChain(r.Context(), inputs.Spot)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "chain: " + err.Error()})
		return
	}
	selected, reason, ok := s.optionsAlgoSvc.SelectContract(r.Context(), direction.Direction, chain)
	writeJSON(w, http.StatusOK, map[string]any{
		"direction":   direction.Direction,
		"spot":        inputs.Spot,
		"chain":       chain,
		"selected":    selected,
		"selected_ok": ok,
		"reason":      reason,
	})
}

// GET /v1/admin/optionsalgo/entry — live-verification for Phase 3's full
// direction -> chain -> select -> entry pipeline. Read-only, no trade is
// placed or affected. Admin only.
func (s *Server) handleOptionsAlgoEntry(w http.ResponseWriter, r *http.Request) {
	direction, inputs, err := s.optionsAlgoSvc.EvaluateDirection(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "direction: " + err.Error()})
		return
	}
	chain, err := s.optionsAlgoSvc.BuildOptionChain(r.Context(), inputs.Spot)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "chain: " + err.Error()})
		return
	}
	selected, selectionReason, ok := s.optionsAlgoSvc.SelectContract(r.Context(), direction.Direction, chain)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"direction":        direction.Direction,
			"direction_reason": direction.Reason,
			"selection_reason": selectionReason,
			"entry":            nil,
		})
		return
	}
	entry, err := s.optionsAlgoSvc.EvaluateEntryForSelected(r.Context(), direction.Direction, inputs, selected)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "entry: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"direction":        direction.Direction,
		"direction_reason": direction.Reason,
		"selected":         selected,
		"selection_reason": selectionReason,
		"entry":            entry,
	})
}

// POST /v1/admin/optionsalgo/enter?user_id=... — manually fire one
// evaluate-and-maybe-enter pass for Phase 4b's execution bridge. Places a
// REAL paper trade under the given user's algo balance if every check
// clears — this is the actual execution path, not a read-only preview like
// the other optionsalgo debug endpoints. Admin only, used to verify the
// pipeline live before it's wired into the automatic per-minute loop.
func (s *Server) handleOptionsAlgoEnter(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id query param required"})
		return
	}
	out, err := s.optionsAlgoSvc.EvaluateAndMaybeEnter(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/admin/optionsalgo/manage?user_id=... — manually fire one
// management tick over the given user's open algo positions. Admin only.
// POST /v1/admin/optionsalgo/archive-chain — fire one option-chain snapshot
// on demand. The archiver normally runs off the per-minute polling tick,
// which is gated on market hours, so this exists to verify capture outside
// them. Same "admin-only manual testing, no real callers" pattern as the
// other /admin/optionsalgo debug endpoints.
func (s *Server) handleArchiveChainSnapshot(w http.ResponseWriter, r *http.Request) {
	s.optionsAlgoSvc.ArchiveChainSnapshot(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "archive attempted — see option_chain_snapshots and the server log for the outcome",
	})
}

func (s *Server) handleOptionsAlgoManage(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id query param required"})
		return
	}
	out, err := s.optionsAlgoSvc.ManageOpenPositions(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"outcomes": out})
}

// GET /v1/admin/optionsalgo/config — every tunable script value (risk%, stop/
// breakeven/trailing %, delta band, spread/volume filters, EMA/ATR periods,
// OR window, VWAP distance limit, strikes-each-side, max trades/day). Admin
// only.
func (s *Server) handleGetAlgoConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.optionsAlgo.GetConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// PUT /v1/admin/optionsalgo/config — overwrites the single config row. The
// frontend settings form sends the full config back (mirroring what
// handleGetAlgoConfig returned), avoiding partial-update ambiguity. Admin
// only.
func (s *Server) handleUpdateAlgoConfig(w http.ResponseWriter, r *http.Request) {
	var cfg optionsalgo.AlgoConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.optionsAlgo.UpdateConfig(r.Context(), cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// GET /v1/admin/optionsalgo/decisions?limit=50 — the algo's full decision/
// audit log, newest first: every evaluation tick (traded or not) and every
// exit, with the full context that produced it. Admin only.
func (s *Server) handleOptionsAlgoDecisions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	decisions, err := s.optionsAlgo.RecentDecisions(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": decisions})
}

// GET /v1/admin/optionsalgo/candles — the 1-minute candle history stored for
// each tracked options-algo underlying (Nifty 50, SENSEX): latest bar time,
// total bar count, and the most recent bars. Proves the backfill/live-refresh
// loop (internal/optionsalgo) is actually populating data. Admin only.
func (s *Server) handleOptionsAlgoCandles(w http.ResponseWriter, r *http.Request) {
	underlyings, err := s.optionsAlgo.TrackedUnderlyings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type underlyingCandles struct {
		Symbol           string          `json:"symbol"`
		LatestCandleTime time.Time       `json:"latest_candle_time"`
		Count            int             `json:"count"`
		RecentBars       []market.Candle `json:"recent_bars"`
	}
	out := make([]underlyingCandles, 0, len(underlyings))
	for _, u := range underlyings {
		count, latest, err := s.optionsAlgo.Stats(r.Context(), u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		bars, err := s.optionsAlgo.GetMinuteCandles(r.Context(), u.ID, 15)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out = append(out, underlyingCandles{
			Symbol:           u.TradingSymbol,
			LatestCandleTime: latest,
			Count:            count,
			RecentBars:       bars,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"underlyings": out})
}

// parseAdminDate reads the required ?date=YYYY-MM-DD query param (IST).
func parseAdminDate(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	ds := r.URL.Query().Get("date")
	if ds == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date query param required (YYYY-MM-DD)"})
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", ds, market.IST)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date; use YYYY-MM-DD"})
		return time.Time{}, false
	}
	return d, true
}

// GET /v1/admin/candles?date=YYYY-MM-DD — how many instruments have a candle
// stored on that date. Doubles as the missing-candle diagnostic. Admin only.
func (s *Server) handleCandleCountByDate(w http.ResponseWriter, r *http.Request) {
	d, ok := parseAdminDate(w, r)
	if !ok {
		return
	}
	n, err := s.engine.CountCandlesForDate(r.Context(), d)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":           d.Format("2006-01-02"),
		"weekday":        d.Weekday().String(),
		"is_trading_day": s.cal.Cal().IsTradingDay(d),
		"count":          n,
	})
}

// DELETE /v1/admin/candles?date=YYYY-MM-DD — remove every daily candle on that
// date and rebuild affected aggregates. Admin only.
func (s *Server) handleDeleteCandlesByDate(w http.ResponseWriter, r *http.Request) {
	d, ok := parseAdminDate(w, r)
	if !ok {
		return
	}
	deleted, err := s.engine.DeleteCandlesForDate(r.Context(), d)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":    d.Format("2006-01-02"),
		"deleted": deleted,
	})
}

// POST /v1/admin/candles/refetch?date=YYYY-MM-DD — re-fetch that trading day
// from Angel for every tracked instrument (background; one rate-limited call
// per stock). Admin only. Returns 202.
func (s *Server) handleRefetchCandlesByDate(w http.ResponseWriter, r *http.Request) {
	d, ok := parseAdminDate(w, r)
	if !ok {
		return
	}
	if !s.refetchRunning.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "already_running", "message": "a refetch is already in progress",
		})
		return
	}
	go func() {
		defer s.refetchRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		res, err := s.engine.RefetchDate(ctx, d)
		if err != nil {
			s.log.Error().Err(err).Str("date", d.Format("2006-01-02")).Msg("admin refetch failed")
			return
		}
		s.log.Info().Str("date", res.Date).Int("updated", res.Updated).Msg("admin refetch done")
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"date":    d.Format("2006-01-02"),
		"message": "refetch started in background",
	})
}

// POST /v1/admin/dispatch/force?signal_id=1 — re-send a stored signal to all of
// its current recipients + the safety-net chat, IGNORING dedup and the freshness
// window. Backs the admin "fire again" button in the Audit view. Admin only.
func (s *Server) handleForceDispatch(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications disabled (NOTIFY_ENABLED=false)"})
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("signal_id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signal_id query param required"})
		return
	}
	sig, err := s.signals.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	res, err := s.notifier.ForceResend(r.Context(), sig)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
