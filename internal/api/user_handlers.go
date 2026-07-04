package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/users"
)

// POST /v1/users  {"email":"a@b.com"}
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email required"})
		return
	}
	id, err := s.users.CreateUser(r.Context(), body.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "email": body.Email})
}

// GET /v1/users
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.users.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "users": list})
}

// POST /v1/users/{uid}/watchlists  {"name":"My List"}
func (s *Server) handleCreateWatchlist(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	id, err := s.users.CreateWatchlist(r.Context(), uid, body.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "name": body.Name})
}

// GET /v1/users/{uid}/watchlists
func (s *Server) handleListWatchlists(w http.ResponseWriter, r *http.Request) {
	list, err := s.users.ListWatchlists(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "watchlists": list})
}

// POST /v1/watchlists/{wid}/items  {"instrument_id":1}
func (s *Server) handleAddWatchlistItem(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "wid")
	var body struct {
		InstrumentID int64 `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InstrumentID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instrument_id required"})
		return
	}
	if err := s.users.AddWatchlistItem(r.Context(), wid, body.InstrumentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"watchlist_id": wid, "instrument_id": body.InstrumentID})
}

// DELETE /v1/watchlists/{wid}/items/{instrumentId}
func (s *Server) handleRemoveWatchlistItem(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "wid")
	id, err := strconv.ParseInt(chi.URLParam(r, "instrumentId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	if err := s.users.RemoveWatchlistItem(r.Context(), wid, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// PUT /v1/users/{uid}/scanner-prefs  {"prefs":{"pine_1d":true,"weekly_1":true}}
func (s *Server) handleSetScannerPrefs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prefs map[string]bool `json:"prefs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Prefs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prefs map required"})
		return
	}
	if err := s.users.SetScannerPrefs(r.Context(), chi.URLParam(r, "uid"), body.Prefs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(body.Prefs)})
}

// GET /v1/users/{uid}/scanner-prefs
func (s *Server) handleGetScannerPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.users.GetScannerPrefs(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prefs": prefs})
}

// PUT /v1/users/{uid}/telegram  {"bot_token":"...","chat_id":"...","enabled":true}
func (s *Server) handleSetTelegram(w http.ResponseWriter, r *http.Request) {
	var cfg users.TelegramConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.users.SetTelegram(r.Context(), chi.URLParam(r, "uid"), cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// GET /v1/users/{uid}/telegram
func (s *Server) handleGetTelegram(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.users.GetTelegram(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
