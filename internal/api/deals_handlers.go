package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/deals"
)

// dealTypeFrom maps a URL path prefix to a deal type. Returns ok=false for an
// unknown type.
func dealTypeFrom(s string) (deals.Type, bool) {
	t := deals.Type(s)
	return t, t.Valid()
}

// GET /v1/{bulk|block}-deals — card summaries for the deal type, newest first.
func (s *Server) handleListDeals(t deals.Type) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deals == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
			return
		}
		list, err := s.deals.ListStocks(r.Context(), t)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "stocks": list})
	}
}

// GET /v1/{bulk|block}-deals/{symbol} — per-client nets + raw rows for one stock.
func (s *Server) handleDealDetail(t deals.Type) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deals == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
			return
		}
		symbol := chi.URLParam(r, "symbol")
		detail, err := s.deals.GetStock(r.Context(), t, symbol)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

// GET /v1/{bulk|block}-deals/audit — the sent-alert ledger for the deal type.
func (s *Server) handleDealsAudit(t deals.Type) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deals == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
			return
		}
		list, err := s.deals.ListAudit(r.Context(), t)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "alerts": list})
	}
}

// POST /v1/admin/deals/{type}/{symbol}/send-alert — force-send one stock's
// alert (most recent stored day), ignoring the alert ledger. Admin only.
func (s *Server) handleDealsSendAlert(w http.ResponseWriter, r *http.Request) {
	if s.deals == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "deals tracking disabled"})
		return
	}
	t, ok := dealTypeFrom(chi.URLParam(r, "type"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be bulk or block"})
		return
	}
	symbol := chi.URLParam(r, "symbol")
	if err := s.deals.SendAlert(r.Context(), t, symbol); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "type": t, "symbol": symbol})
}
