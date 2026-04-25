package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/farigab/bragdev-go/internal/middleware"
	"github.com/farigab/bragdev-go/internal/repository"
	"github.com/farigab/bragdev-go/internal/validation"
)

// RegisterTokenRoutes registers GitHub token management endpoints.
// Must be called on a router that already applies AuthWithRefresh so that
// the user login is available in context.
func RegisterTokenRoutes(r chi.Router, userRepo repository.UserRepository) {
	h := &tokenHandler{userRepo: userRepo}
	r.Post("/api/auth/github/token", h.handleSaveGitHubToken)
	r.Delete("/api/auth/github/token", h.handleClearGitHubToken)
}

type tokenHandler struct {
	userRepo repository.UserRepository
}

func (h *tokenHandler) handleSaveGitHubToken(w http.ResponseWriter, r *http.Request) {
	login, ok := middleware.UserLoginFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Validate before touching SQL — driver has no prepared statements.
	if err := validation.ValidateGitHubToken(payload.Token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// UpdateGitHubToken issues a targeted UPDATE so name/avatar_url are never
	// overwritten — the previous Save(&User{Login: login, Token: t}) approach
	// silently zeroed those columns on conflict.
	if err := h.userRepo.UpdateGitHubToken(r.Context(), login, payload.Token); err != nil {
		http.Error(w, "failed to save token", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *tokenHandler) handleClearGitHubToken(w http.ResponseWriter, r *http.Request) {
	login, ok := middleware.UserLoginFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.userRepo.ClearGitHubToken(r.Context(), login); err != nil {
		http.Error(w, "failed to clear token", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
