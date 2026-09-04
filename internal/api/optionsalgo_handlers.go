package api

import "net/http"

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
