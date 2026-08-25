package api

import "net/http"

// POST /v1/admin/digest/send-now — trigger the weekly digest immediately,
// for testing without waiting on DIGEST_CRON.
func (s *Server) handleSendDigestNow(w http.ResponseWriter, r *http.Request) {
	if s.digest == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "digest not configured (set DIGEST_ENABLED, SMTP_* and ensure promoter+deals tracking are both enabled)"})
		return
	}
	sent, failed, err := s.digest.SendWeekly(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "failed": failed})
}
