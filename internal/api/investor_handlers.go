package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GET /v1/big-investors — one card per tracked big investor with at least
// one currently disclosed holding.
func (s *Server) handleListInvestors(w http.ResponseWriter, r *http.Request) {
	if s.investors == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "big investor tracking disabled"})
		return
	}
	list, err := s.investors.ListInvestors(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "investors": list})
}

// GET /v1/big-investors/{name} — every stock one tracked investor currently
// holds. {name} is the investor_name from the list endpoint, URL-encoded by
// the caller.
func (s *Server) handleInvestorDetail(w http.ResponseWriter, r *http.Request) {
	if s.investors == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "big investor tracking disabled"})
		return
	}
	name := chi.URLParam(r, "name")
	holdings, err := s.investors.GetInvestor(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"investor_name": name, "holdings": holdings})
}

// POST /v1/admin/big-investors/refresh — poll the NSE shareholding-pattern
// feed immediately. Admin only, cooldown-guarded (see Service.RefreshNow).
// The poll itself runs in the background: the very first-ever run walks a
// full quarter of filings (see investors.catchUpWindow) and can take a
// while, so the response doesn't wait for it.
func (s *Server) handleRefreshInvestors(w http.ResponseWriter, r *http.Request) {
	if s.investors == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "big investor tracking disabled"})
		return
	}
	started, err := s.investors.RefreshNow()
	if err != nil {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	if started {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "refreshing"})
	}
}
