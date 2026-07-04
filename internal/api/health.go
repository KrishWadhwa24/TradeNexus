package api

import (
	"context"
	"net/http"
	"time"
)

// handleHealth is a liveness probe: the process is up and serving.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady is a readiness probe: dependencies (Postgres, Redis) are reachable.
// Returns 200 when both are healthy, 503 otherwise with per-dependency detail.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	if err := s.pg.Ping(ctx); err != nil {
		checks["postgres"] = "down: " + err.Error()
		healthy = false
	} else {
		checks["postgres"] = "ok"
	}

	if err := s.rdb.Ping(ctx); err != nil {
		checks["redis"] = "down: " + err.Error()
		healthy = false
	} else {
		checks["redis"] = "ok"
	}

	status := http.StatusOK
	overall := "ready"
	if !healthy {
		status = http.StatusServiceUnavailable
		overall = "not_ready"
	}
	writeJSON(w, status, map[string]any{"status": overall, "checks": checks})
}
