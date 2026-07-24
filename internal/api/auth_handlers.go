package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"tradenexus/internal/auth"
	"tradenexus/internal/users"
)

type authBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// POST /v1/auth/register {email,password}
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var b authBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Email == "" || len(b.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password (min 6 chars) required"})
		return
	}
	hash, err := auth.HashPassword(b.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	id, err := s.users.Register(r.Context(), b.Email, hash)
	if err != nil {
		if errors.Is(err, users.ErrEmailTaken) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.issueToken(w, id, b.Email, false) // self-registration is never admin
}

// POST /v1/auth/login {email,password}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var b authBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Email == "" || b.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}
	id, hash, isAdmin, err := s.users.AuthByEmail(r.Context(), b.Email)
	if err != nil || !auth.CheckPassword(hash, b.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}
	s.issueToken(w, id, b.Email, isAdmin)
}

// GET /v1/me — current user from the token.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(userIDKey).(string)
	isAdmin, _ := r.Context().Value(isAdminKey).(bool)
	writeJSON(w, http.StatusOK, map[string]any{"id": uid, "is_admin": isAdmin})
}

func (s *Server) issueToken(w http.ResponseWriter, id, email string, isAdmin bool) {
	token, err := auth.Issue(s.jwtSecret, id, email, isAdmin)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]any{"id": id, "email": email, "is_admin": isAdmin},
	})
}
