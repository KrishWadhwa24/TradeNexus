package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"tradenexus/internal/auth"
	"tradenexus/internal/instruments"
)

var liveUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

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

	for {
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case tick, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(tick); err != nil {
				return
			}
		}
	}
}

func (s *Server) liveInstruments(r *http.Request, uid string) ([]instruments.Instrument, error) {
	wls, err := s.users.ListWatchlists(r.Context(), uid)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var out []instruments.Instrument
	for _, wl := range wls {
		for _, id := range wl.InstrumentIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			it, err := s.inst.GetByID(r.Context(), id)
			if err != nil {
				continue
			}
			out = append(out, it)
		}
	}
	return out, nil
}
