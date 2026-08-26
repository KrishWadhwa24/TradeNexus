package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GET /v1/promoter-buying — one card per stock with tracked promoter/KMP
// positions, ranked by combined stake point-increase (largest first).
func (s *Server) handleListPromoterBuying(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	list, err := s.promoter.ListStockBuying(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "stocks": list})
}

// GET /v1/promoter-buying/{symbol} — every tracked person's position for one
// stock (the detail-modal source).
func (s *Server) handlePromoterBuyingDetail(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	symbol := chi.URLParam(r, "symbol")
	detail, err := s.promoter.GetStockBuying(r.Context(), symbol)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// GET /v1/promoter-buying/{symbol}/history?person=<name> — one person's
// individual disclosure history for one stock (rate/qty/date per
// transaction). Bound by the raw feed's retention window — see
// promoter.Repo.ListForPerson.
func (s *Server) handlePromoterPersonHistory(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	symbol := chi.URLParam(r, "symbol")
	person := r.URL.Query().Get("person")
	if person == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "person query param required"})
		return
	}
	trades, err := s.promoter.PersonHistory(r.Context(), symbol, person)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(trades), "trades": trades})
}

// POST /v1/admin/promoter-buying/backfill — one-time seed of
// promoter_positions from whatever's currently in promoter_trades. Admin
// only; safe to re-run (see promoter.Repo.BackfillPositions).
func (s *Server) handleBackfillPromoterBuying(w http.ResponseWriter, r *http.Request) {
	if s.promoter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "promoter tracking disabled"})
		return
	}
	if err := s.promoter.BackfillPositions(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "done"})
}
