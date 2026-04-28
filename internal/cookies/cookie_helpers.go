// Package cookies provides shared HTTP cookie helpers used across handlers and middleware.
package cookies

import (
	"net/http"

	"bragdev-go/internal/config"
)

// Set writes an HttpOnly cookie with settings derived from cfg.
// Use maxAge=-1 to delete the cookie.
func Set(w http.ResponseWriter, name, value string, maxAge int, cfg *config.Config) {
	secure := true
	sameSite := http.SameSiteLaxMode
	var domain string
	if cfg != nil {
		secure = cfg.CookieSecure
		sameSite = ParseSameSite(cfg.CookieSameSite)
		domain = cfg.CookieDomain
	}
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		HttpOnly: true,
		Secure:   secure,
		Path:     "/",
		MaxAge:   maxAge,
		SameSite: sameSite,
	}
	// Setting Domain for localhost breaks cookies in most browsers.
	if domain != "" && domain != "localhost" {
		c.Domain = domain
	}
	http.SetCookie(w, c)
}

// ClearAuth deletes the token and refreshToken cookies.
func ClearAuth(w http.ResponseWriter, cfg *config.Config) {
	Set(w, "token", "", -1, cfg)
	Set(w, "refreshToken", "", -1, cfg)
}

// ParseSameSite converts the string config value to http.SameSite.
func ParseSameSite(s string) http.SameSite {
	switch s {
	case "Strict":
		return http.SameSiteStrictMode
	case "None":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
