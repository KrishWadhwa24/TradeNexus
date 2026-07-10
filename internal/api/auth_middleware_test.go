package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"tradenexus/internal/auth"
)

func TestAuthMiddleware(t *testing.T) {
	s := &Server{jwtSecret: "test-secret"}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := s.authMiddleware(next)

	// No Authorization header → 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: got %d, want 401", rec.Code)
	}

	// Malformed/invalid token → 401.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: got %d, want 401", rec.Code)
	}

	// Valid token → passes through to next handler.
	tok, err := auth.Issue("test-secret", "user-1", "a@b.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("valid token: got %d body=%q, want 200 ok", rec.Code, rec.Body.String())
	}

	// Token signed with a different secret → 401.
	badTok, _ := auth.Issue("other-secret", "user-1", "a@b.com")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+badTok)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-secret token: got %d, want 401", rec.Code)
	}
}
