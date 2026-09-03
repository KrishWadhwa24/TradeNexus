package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GET /v1/mutual-funds — one card per mutual fund seen as a bulk/block deal
// client, ever (permanent position, not retention-windowed).
func (s *Server) handleListMutualFunds(w http.ResponseWriter, r *http.Request) {
	if s.deals == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
		return
	}
	list, err := s.deals.ListFunds(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "funds": list})
}

// GET /v1/mutual-funds/{fund} — per-stock breakdown for one fund (the
// bar+pie chart source). {fund} is the fund_name from the list endpoint,
// URL-encoded by the caller.
func (s *Server) handleMutualFundDetail(w http.ResponseWriter, r *http.Request) {
	if s.deals == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
		return
	}
	fund := chi.URLParam(r, "fund")
	detail, err := s.deals.GetFund(r.Context(), fund)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// POST /v1/admin/mutual-funds/backfill — one-time seed of mutual_fund_positions
// from whatever's currently in market_deals. Admin only; safe to re-run
// (see Repo.BackfillFundPositions).
func (s *Server) handleBackfillMutualFunds(w http.ResponseWriter, r *http.Request) {
	if s.deals == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
		return
	}
	if err := s.deals.BackfillFundPositions(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "done"})
}
