package api

import (
	"net/http"
)

// handleRateLimitTry consumes one token from the shared Angel bucket and reports
// whether it was allowed. Purely a Module-1 test aid: fire it repeatedly in
// Postman to see the token bucket deny and hand back a retry hint.
//
// NOTE: this shares the SAME bucket the Angel fetcher will use, so don't leave
// it hammering while real syncs run.
func (s *Server) handleRateLimitTry(w http.ResponseWriter, r *http.Request) {
	ok, retry, err := s.limiter.Allow(r.Context(), 1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":        ok,
		"retry_after_ms": retry.Milliseconds(),
	})
}
