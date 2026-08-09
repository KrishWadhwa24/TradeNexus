package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/candles"
	"tradenexus/internal/instruments"
)

// GET /v1/admin/featured-stocks — the admin-curated list shown on the public
// landing page, for the Admin dashboard's management UI.
func (s *Server) handleListFeaturedStocks(w http.ResponseWriter, r *http.Request) {
	items, err := s.inst.ListFeatured(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "max": instruments.MaxFeatured, "stocks": items})
}

// POST /v1/admin/featured-stocks — add a stock to the featured list (max 10).
// Body: {"instrument_id": 123}
func (s *Server) handleAddFeaturedStock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstrumentID int64 `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InstrumentID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instrument_id is required"})
		return
	}
	if err := s.inst.AddFeatured(r.Context(), req.InstrumentID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, instruments.ErrFeaturedFull) || errors.Is(err, instruments.ErrAlreadyFeatured) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	// A stock picked here may never have been on any watchlist, so it may have
	// no candle history yet — without it, the landing page silently drops it
	// (no price/RSI to show). Sync in the background so adding it just works,
	// the same way Watchlist.jsx already syncs history right after adding.
	id := req.InstrumentID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.syncCandles(ctx, id, candles.RequiredDailyBars); err != nil {
			s.log.Error().Err(err).Int64("instrument_id", id).Msg("featured stocks: candle sync failed")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// DELETE /v1/admin/featured-stocks/{id} — remove a stock from the featured list.
func (s *Server) handleRemoveFeaturedStock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	if err := s.inst.RemoveFeatured(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
