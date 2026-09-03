package api

import (
	"net/http"
	"strconv"
)

// GET /v1/insights/performance — scanner forward-return stats.
func (s *Server) handleInsightsPerformance(w http.ResponseWriter, r *http.Request) {
	if s.insights == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "insights disabled"})
		return
	}
	perf, err := s.insights.SignalPerformance(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(perf), "scanners": perf})
}

// GET /v1/insights/breadth?days=30 — daily BUY vs SELL signal counts.
func (s *Server) handleInsightsBreadth(w http.ResponseWriter, r *http.Request) {
	if s.insights == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "insights disabled"})
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	pts, err := s.insights.Breadth(r.Context(), days)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(pts), "days": days, "points": pts})
}

// GET /v1/insights/confluence — stocks where 2+ bullish sources align.
func (s *Server) handleInsightsConfluence(w http.ResponseWriter, r *http.Request) {
	if s.insights == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "insights disabled"})
		return
	}
	board, err := s.insights.Confluence(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(board), "stocks": board})
}
