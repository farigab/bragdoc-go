// Package handlers contains HTTP handler registrations and implementations.
package handlers

import (
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/farigab/bragdev-go/internal/config"
	"github.com/farigab/bragdev-go/internal/cookies"
	"github.com/farigab/bragdev-go/internal/domain"
	"github.com/farigab/bragdev-go/internal/integration"
	"github.com/farigab/bragdev-go/internal/logger"
	"github.com/farigab/bragdev-go/internal/repository"
	"github.com/farigab/bragdev-go/internal/security"
)

const (
	loginErrorURL = "/login?error=auth_failed"
	maxBodyBytes  = 1 << 20 // 1 MiB
)

// authHandler holds dependencies for all auth routes.
type authHandler struct {
	cfg         *config.Config
	oauth       integration.OAuthService
	jwtSvc      security.TokenService
	userRepo    repository.UserRepository
	refreshRepo repository.RefreshTokenRepository
}

// RegisterAuthRoutes registers public authentication endpoints (OAuth flow + token refresh).
func RegisterAuthRoutes(r chi.Router, cfg *config.Config, oauth integration.OAuthService, jwtSvc security.TokenService, userRepo repository.UserRepository, refreshRepo repository.RefreshTokenRepository) {
	h := &authHandler{cfg, oauth, jwtSvc, userRepo, refreshRepo}

	r.Get("/api/auth/github", h.handleGitHubLogin)
	r.Get("/api/auth/callback", h.handleGitHubCallback)
	r.Post("/api/auth/refresh", h.handleRefresh)
	r.Post("/api/auth/logout", h.handleLogout)
}

func (h *authHandler) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	state := uuid.New().String()

	// Use cookies.Set so domain/secure/samesite settings come from config,
	// consistent with every other cookie this app issues.
	cookies.Set(w, "oauth_state", state, 300, h.cfg)

	q := url.Values{}
	q.Set("client_id", h.cfg.GitHubClientID)
	q.Set("redirect_uri", h.resolveRedirectURI())
	q.Set("scope", "read:user,user:email")
	q.Set("state", state)

	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

func (h *authHandler) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if err := h.validateOAuthState(w, r); err != nil {
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code missing", http.StatusBadRequest)
		return
	}

	accessToken, err := h.oauth.ExchangeCodeForToken(code, h.resolveRedirectURI())
	if err != nil {
		logger.Errorw("error exchanging code", "err", err)
		h.redirectLoginError(w, r)
		return
	}

	savedUser, err := h.upsertUserFromOAuth(r, accessToken)
	if err != nil {
		h.redirectLoginError(w, r)
		return
	}

	if err := h.issueAuthCookies(r, w, savedUser); err != nil {
		h.redirectLoginError(w, r)
		return
	}

	logger.Infow("user authenticated", "login", savedUser.Login)
	http.Redirect(w, r, h.cfg.FrontendRedirectURI, http.StatusFound)
}

func (h *authHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refreshToken")
	if err != nil || cookie.Value == "" {
		cookies.ClearAuth(w, h.cfg)
		http.Error(w, "no refresh token", http.StatusUnauthorized)
		return
	}

	oldRt, err := h.refreshRepo.FindByToken(r.Context(), cookie.Value)
	if err != nil {
		cookies.ClearAuth(w, h.cfg)
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}

	if oldRt.Revoked || time.Now().After(oldRt.ExpiresAt) {
		cookies.ClearAuth(w, h.cfg)
		http.Error(w, "refresh token expired", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.FindByLogin(r.Context(), oldRt.UserLogin)
	if err != nil {
		cookies.ClearAuth(w, h.cfg)
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	if err := h.rotateTokens(r, w, user, oldRt); err != nil {
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *authHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if login := h.loginFromCookie(r); login != "" {
		_ = h.refreshRepo.DeleteAllByUserLogin(r.Context(), login)
	}
	cookies.ClearAuth(w, h.cfg)
	w.WriteHeader(http.StatusOK)
}

// --- private helpers ---------------------------------------------------------

func (h *authHandler) validateOAuthState(w http.ResponseWriter, r *http.Request) error {
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return err
	}
	stateParam := r.URL.Query().Get("state")
	if stateParam == "" || stateParam != stateCookie.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return http.ErrNoCookie
	}
	// Clear the state cookie after validation.
	cookies.Set(w, "oauth_state", "", -1, h.cfg)
	return nil
}

func (h *authHandler) upsertUserFromOAuth(r *http.Request, accessToken string) (*domain.User, error) {
	profile, err := h.oauth.GetUserProfile(accessToken)
	if err != nil {
		logger.Errorw("error fetching profile", "err", err)
		return nil, err
	}

	login, _ := profile["login"].(string)
	name, _ := profile["name"].(string)
	avatar, _ := profile["avatar_url"].(string)
	if name == "" {
		name = login
	}

	user := domain.NewUser(login, name, avatar)
	saved, err := h.userRepo.Save(r.Context(), user)
	if err != nil {
		logger.Errorw("error saving user", "err", err)
		return nil, err
	}
	return saved, nil
}

func (h *authHandler) issueAuthCookies(r *http.Request, w http.ResponseWriter, user *domain.User) error {
	jwtToken, err := h.jwtSvc.GenerateToken(user.Login, map[string]interface{}{
		"name":   user.Name,
		"avatar": user.AvatarURL,
	})
	if err != nil {
		logger.Errorw("error generating jwt", "err", err)
		return err
	}

	refreshTokenStr, err := h.createRefreshToken(r, user.Login)
	if err != nil {
		logger.Errorw("error saving refresh token", "err", err)
		return err
	}

	cookies.Set(w, "token", jwtToken, 15*60, h.cfg)
	cookies.Set(w, "refreshToken", refreshTokenStr, 7*24*60*60, h.cfg)
	return nil
}

func (h *authHandler) rotateTokens(r *http.Request, w http.ResponseWriter, user *domain.User, oldRt *domain.RefreshToken) error {
	jwtToken, err := h.jwtSvc.GenerateToken(user.Login, map[string]interface{}{
		"name":   user.Name,
		"avatar": user.AvatarURL,
	})
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return err
	}

	newToken, err := h.createRefreshToken(r, user.Login)
	if err != nil {
		http.Error(w, "failed to save refresh token", http.StatusInternalServerError)
		return err
	}

	_ = h.refreshRepo.Delete(r.Context(), oldRt)

	cookies.Set(w, "token", jwtToken, 15*60, h.cfg)
	cookies.Set(w, "refreshToken", newToken, 7*24*60*60, h.cfg)
	return nil
}

func (h *authHandler) createRefreshToken(r *http.Request, userLogin string) (string, error) {
	token := uuid.New().String()
	rt := domain.NewRefreshToken(token, userLogin, time.Now().Add(7*24*time.Hour))
	if _, err := h.refreshRepo.Save(r.Context(), rt); err != nil {
		return "", err
	}
	return token, nil
}

// loginFromCookie extracts the user login from the JWT cookie.
// Uses ExtractUserLoginSafe so that an expired token still yields the login —
// critical for logout paths where we must revoke refresh tokens even when the
// access token has already expired.
func (h *authHandler) loginFromCookie(r *http.Request) string {
	cookie, err := r.Cookie("token")
	if err != nil || cookie.Value == "" {
		return ""
	}

	type safeExtractor interface {
		ExtractUserLoginSafe(string) (string, error)
	}

	if svc, ok := h.jwtSvc.(safeExtractor); ok {
		login, _ := svc.ExtractUserLoginSafe(cookie.Value)
		return login
	}

	// Fallback for implementations that don't expose ExtractUserLoginSafe.
	login, _ := h.jwtSvc.ExtractUserLogin(cookie.Value)
	return login
}

func (h *authHandler) resolveRedirectURI() string {
	if h.cfg.GitHubRedirectURI != "" {
		return h.cfg.GitHubRedirectURI
	}
	return "http://localhost:8080/api/auth/callback"
}

func (h *authHandler) redirectLoginError(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, h.cfg.FrontendRedirectURI+loginErrorURL, http.StatusFound)
}
