package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"tradenexus/internal/analytics"
	"tradenexus/internal/auth"
	"tradenexus/internal/instruments"
)

var liveUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// liveHeartbeat is how often an otherwise-idle browser<->server live-prices
// connection gets a keepalive frame (e.g. an empty watchlist, or a market
// that's simply quiet). Some proxies/load balancers — and Vite's own dev
// WebSocket proxy — silently drop a socket that's carried zero bytes for a
// couple of minutes; a small periodic frame is cheap insurance against that
// class of "why did this reconnect for no reason" bug.
const liveHeartbeat = 30 * time.Second

// GET /v1/users/{uid}/live-prices?token=jwt
func (s *Server) handleLivePrices(w http.ResponseWriter, r *http.Request) {
	if s.live == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "live prices not configured"})
		return
	}
	uid := chi.URLParam(r, "uid")
	claims, err := auth.Parse(s.jwtSecret, r.URL.Query().Get("token"))
	if err != nil || claims.UserID != uid {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
		return
	}

	items, err := s.liveInstruments(r, uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ch, cancel, err := s.live.Subscribe(r.Context(), items)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	conn, err := liveUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn().Err(err).Str("uid", uid).Msg("live-prices: websocket upgrade failed")
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
				// Nothing to stream (e.g. an empty watchlist) — go idle rather
				// than closing. Closing here just triggers the frontend's
				// auto-reconnect, which would immediately hit this same empty
				// channel again: a tight reconnect loop for no benefit. Nil-ing
				// the channel makes this case never select again, without
				// busy-looping on the already-closed channel.
				ch = nil
				continue
			}
			if err := conn.WriteJSON(tick); err != nil {
				return
			}
		}
	}
}

// GET /v1/public/live-prices — PUBLIC (no auth) real-time price stream for a
// small set of stocks, for the pre-login landing page. Reuses the same Angel
// live hub as the authenticated dashboard feed — real ticks, no polling.
// Shows the admin-curated featured list (see internal/instruments/featured.go)
// so every visitor sees the same deliberately-chosen stocks; falls back to
// algorithmic top-movers only if the admin hasn't curated a list yet, so the
// landing page is never empty by default.
func (s *Server) handlePublicLivePrices(w http.ResponseWriter, r *http.Request) {
	if s.live == nil || s.analytics == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "live prices not configured"})
		return
	}

	items, err := s.inst.ListFeatured(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(items) == 0 {
		movers, err := s.analytics.TopMovers(r.Context(), 8)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		for _, m := range movers {
			it, gerr := s.inst.GetByID(r.Context(), m.InstrumentID)
			if gerr != nil {
				continue
			}
			items = append(items, it)
		}
	}

	ch, cancel, err := s.live.Subscribe(r.Context(), items)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	conn, err := liveUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn().Err(err).Msg("public live-prices: websocket upgrade failed")
		cancel()
		return
	}
	defer conn.Close()
	defer cancel()

	// Initial snapshot: real latest params (price, %change, RSI) so the landing
	// renders immediately, then live ticks update the prices. No polling.
	snap := make([]analytics.Params, 0, len(items))
	for _, it := range items {
		p, perr := s.instrumentParams(r, it.ID)
		if perr != nil || !p.HasData {
			continue
		}
		snap = append(snap, p)
	}
	_ = conn.WriteJSON(map[string]any{"type": "snapshot", "rows": snap})

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
				// forcing a client reconnect loop) when there's nothing to
				// stream.
				ch = nil
				continue
			}
			if err := conn.WriteJSON(tick); err != nil {
				return
			}
		}
	}
}

// liveInstruments builds the subscription list for a user's live-prices
// WebSocket: every instrument on any of their watchlists, plus every
// instrument backing a currently OPEN paper trade — so unrealized P&L on
// the Paper Trading page moves live even for a symbol that isn't also on
// a watchlist.
func (s *Server) liveInstruments(r *http.Request, uid string) ([]instruments.Instrument, error) {
	wls, err := s.users.ListWatchlists(r.Context(), uid)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var out []instruments.Instrument
	addID := func(id int64) {
		if seen[id] {
			return
		}
		seen[id] = true
		if it, err := s.inst.GetByID(r.Context(), id); err == nil {
			out = append(out, it)
		}
	}
	for _, wl := range wls {
		for _, id := range wl.InstrumentIDs {
			addID(id)
		}
	}
	if s.paper != nil {
		if ids, err := s.paper.OpenInstrumentIDs(r.Context(), uid); err == nil {
			for _, id := range ids {
				addID(id)
			}
		}
	}
	return out, nil
}
