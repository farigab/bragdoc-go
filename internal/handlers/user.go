package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"bragdev-go/internal/logger"
	"bragdev-go/internal/middleware"
	"bragdev-go/internal/repository"
)

// RegisterUserRoutes registers user-related endpoints.
// The router r must already have the Auth middleware applied.
func RegisterUserRoutes(r chi.Router, userRepo repository.UserRepository) {
	r.Get("/api/user", func(w http.ResponseWriter, req *http.Request) {
		userLogin, ok := middleware.UserLoginFromContext(req.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		u, err := userRepo.FindByLogin(req.Context(), userLogin)
		if err != nil {
			logger.Errorw("user not found", "login", userLogin, "err", err)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		resp := map[string]any{
			"login":          u.Login,
			"name":           u.Name,
			"avatarUrl":      u.AvatarURL,
			"hasGitHubToken": u.GitHubAccessToken != "",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
