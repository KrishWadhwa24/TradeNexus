package api

import (
	"net/http"
	"time"

	"tradenexus/internal/analytics"
)

// parseAnalyticsFilter reads shared query params: from, to (YYYY-MM-DD), tf, source.
func parseAnalyticsFilter(r *http.Request) analytics.Filter {
	q := r.URL.Query()
	f := analytics.Filter{Timeframe: q.Get("tf"), Source: q.Get("source")}
	const layout = "2006-01-02"
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(layout, v); err == nil {
			f.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(layout, v); err == nil {
			// inclusive end-of-day
			end := t.Add(24*time.Hour - time.Second)
			f.To = &end
		}
	}
	return f
}

// GET /v1/analytics/summary?from=&to=&tf=&source=
func (s *Server) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	stats, err := s.analytics.Summary(r.Context(), parseAnalyticsFilter(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// GET /v1/analytics/export.xlsx?from=&to=&tf=&source=
func (s *Server) handleAnalyticsExport(w http.ResponseWriter, r *http.Request) {
	f := parseAnalyticsFilter(r)
	stats, err := s.analytics.Summary(r.Context(), f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows, err := s.analytics.Rows(r.Context(), f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	data, err := analytics.BuildWorkbook(stats, rows)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", analytics.ContentDisposition())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
