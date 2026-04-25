// Package middleware provides HTTP middleware used across handlers.
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/farigab/bragdev-go/internal/config"
	"github.com/farigab/bragdev-go/internal/cookies"
	"github.com/farigab/bragdev-go/internal/domain"
	"github.com/farigab/bragdev-go/internal/logger"
	"github.com/farigab/bragdev-go/internal/repository"
	"github.com/farigab/bragdev-go/internal/security"
)

type contextKey string

// UserLoginKey is the context key for the authenticated user login.
const UserLoginKey contextKey = "userLogin"

// Auth validates the JWT cookie and stores the user login in context.
func Auth(jwtSvc security.TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("token")
			if err != nil || cookie.Value == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			userLogin, err := jwtSvc.ExtractUserLogin(cookie.Value)
			if err != nil || userLogin == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserLoginKey, userLogin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthWithRefresh validates the JWT cookie, and on failure attempts to rotate
// using the refreshToken cookie. Issues new cookies on successful rotation.
func AuthWithRefresh(cfg *config.Config, jwtSvc security.TokenService, userRepo repository.UserRepository, refreshRepo repository.RefreshTokenRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userLogin := extractValidLogin(jwtSvc, r); userLogin != "" {
				ctx := context.WithValue(r.Context(), UserLoginKey, userLogin)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			userLogin, err := rotateRefreshToken(cfg, jwtSvc, userRepo, refreshRepo, w, r)
			if err != nil {
				return
			}
			ctx := context.WithValue(r.Context(), UserLoginKey, userLogin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractValidLogin(jwtSvc security.TokenService, r *http.Request) string {
	cookie, err := r.Cookie("token")
	if err != nil || cookie.Value == "" {
		return ""
	}
	userLogin, err := jwtSvc.ExtractUserLogin(cookie.Value)
	if err != nil {
		return ""
	}
	return userLogin
}

func rotateRefreshToken(
	cfg *config.Config,
	jwtSvc security.TokenService,
	userRepo repository.UserRepository,
	refreshRepo repository.RefreshTokenRepository,
	w http.ResponseWriter,
	r *http.Request,
) (string, error) {
	rtCookie, err := r.Cookie("refreshToken")
	if err != nil || rtCookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", fmt.Errorf("missing refresh token cookie")
	}

	oldRt, err := refreshRepo.FindByToken(r.Context(), rtCookie.Value)
	if err != nil {
		cookies.ClearAuth(w, cfg)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", err
	}
	if oldRt.Revoked || time.Now().After(oldRt.ExpiresAt) {
		cookies.ClearAuth(w, cfg)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", fmt.Errorf("refresh token expired or revoked")
	}

	user, err := userRepo.FindByLogin(r.Context(), oldRt.UserLogin)
	if err != nil {
		cookies.ClearAuth(w, cfg)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", err
	}

	jwtToken, err := jwtSvc.GenerateToken(user.Login, map[string]interface{}{
		"name":   user.Name,
		"avatar": user.AvatarURL,
	})
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return "", err
	}

	newToken := uuid.New().String()
	newRt := domain.NewRefreshToken(newToken, user.Login, time.Now().Add(7*24*time.Hour))
	if _, err = refreshRepo.Save(r.Context(), newRt); err != nil {
		http.Error(w, "failed to save refresh token", http.StatusInternalServerError)
		return "", err
	}

	// Delete old token only after new one is saved to prevent replay attacks.
	// If Delete fails the old token will eventually expire (7-day TTL) and the
	// background cleanup will remove it. The new token is already valid, so we
	// log and continue rather than returning a 500 after cookies were issued.
	if err = refreshRepo.Delete(r.Context(), oldRt); err != nil {
		logger.Errorw("failed to delete old refresh token after rotation",
			"old_token", oldRt.Token,
			"user", user.Login,
			"err", err,
		)
	}

	cookies.Set(w, "token", jwtToken, 15*60, cfg)
	cookies.Set(w, "refreshToken", newToken, 7*24*60*60, cfg)
	return user.Login, nil
}

// UserLoginFromContext extracts the authenticated user login from context.
func UserLoginFromContext(ctx context.Context) (string, bool) {
	login, ok := ctx.Value(UserLoginKey).(string)
	return login, ok && login != ""
}
