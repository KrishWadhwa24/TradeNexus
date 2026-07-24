package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminOnly(t *testing.T) {
	s := &Server{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := s.adminOnly(next)

	// No admin flag in context → 403.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/admin/candles", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no admin flag: got %d, want 403", rec.Code)
	}

	// Explicit non-admin → 403.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/candles", nil).
		WithContext(context.WithValue(context.Background(), isAdminKey, false))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin: got %d, want 403", rec.Code)
	}

	// Admin → passes through.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/admin/candles", nil).
		WithContext(context.WithValue(context.Background(), isAdminKey, true))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("admin: got %d body=%q, want 200 ok", rec.Code, rec.Body.String())
	}
}
