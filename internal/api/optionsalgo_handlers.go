package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/auth"
	"tradenexus/internal/instruments"
	"tradenexus/internal/live"
)

// GET /v1/optionsalgo/chain — the live NIFTY option chain (ATM+/-5 strikes,
// CE/PE, real bid/ask/volume/OI/Greeks) plus the current direction read —
// any logged-in user, not admin-gated, so anyone can browse the chain and
// buy manually (via the existing generic POST /users/{uid}/paper/trades/open,
// under their own regular balance — this endpoint itself never places a
// trade).
func (s *Server) handleOptionChainPublic(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"direction": direction.Direction,
		"spot":      inputs.Spot,
		"chain":     chain,
	})
}

// GET /v1/users/{uid}/optionsalgo/chain-stream?token=jwt&ids=1,2,3 — pushes
// live bid/ask/volume/OI ticks (SnapQuote mode) for the instrument IDs the
// frontend already has from its initial GET /optionsalgo/chain fetch, so
// ChainBrowser's numbers move continuously without re-polling. Registered
// outside the JWT-header auth group and authenticated via a query-param
// token instead, plus the same heartbeat/idle-channel handling, for the same
// reason as handleLivePrices: a browser WebSocket can't set an Authorization
// header, and idle proxies silently drop quiet long-lived sockets.
func (s *Server) handleOptionChainStream(w http.ResponseWriter, r *http.Request) {
	if s.live == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "option chain stream not configured"})
		return
	}
	uid := chi.URLParam(r, "uid")
	claims, err := auth.Parse(s.jwtSecret, r.URL.Query().Get("token"))
	if err != nil || claims.UserID != uid {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
		return
	}

	// maxChainStreamIDs caps a single request's fan-out of GetByID lookups —
	// well above any realistic chain size (a full ATM+/-10 window is ~44
	// contracts), just a floor against an absurdly long "ids" query string.
	const maxChainStreamIDs = 200
	rawIDs := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(rawIDs) > maxChainStreamIDs {
		rawIDs = rawIDs[:maxChainStreamIDs]
	}
	var items []instruments.Instrument
	for _, raw := range rawIDs {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			continue
		}
		if it, err := s.inst.GetByID(r.Context(), id); err == nil {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no valid instrument ids"})
		return
	}

	ch, cancel, err := s.live.SubscribeMode(r.Context(), items, live.ModeSnapQuote)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	conn, err := liveUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn().Err(err).Str("uid", uid).Msg("option chain stream: websocket upgrade failed")
		cancel()
		return
	}
	defer conn.Close()
	defer cancel()

	_ = conn.WriteJSON(map[string]any{"type": "ready", "count": len(items)})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	heartbeat := time.NewTicker(liveHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := conn.WriteJSON(map[string]any{"type": "heartbeat"}); err != nil {
				return
			}
		case tick, ok := <-ch:
			if !ok {
				// See handleLivePrices — go idle instead of closing (and
				// forcing a client reconnect loop) when there's nothing left
				// to stream.
				ch = nil
				continue
			}
			if err := conn.WriteJSON(tick); err != nil {
				return
			}
		}
	}
}
