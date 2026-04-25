// Package config handles loading application configuration from environment.
package config

import (
	"os"

	"github.com/joho/godotenv"
)

// Config armazena variáveis de ambiente usadas pela aplicação.
type Config struct {
	SQLiteCloudURL      string
	JwtSecret           string
	GitHubClientID      string
	GitHubClientSecret  string
	GeminiAPIKey        string
	GeminiAPIURL        string
	GeminiModel         string
	GitHubRedirectURI   string
	FrontendRedirectURI string
	CookieDomain        string
	CookieSecure        bool
	CookieSameSite      string
	LogLevel            string
}

// Load carrega variáveis de ambiente (.env opcional) e retorna a configuração.
func Load() *Config {
	_ = godotenv.Load() // .env is optional; ignore load errors

	gitHubRedirect := os.Getenv("GITHUB_OAUTH_REDIRECT_URI")
	if gitHubRedirect == "" {
		gitHubRedirect = "http://localhost:8080/api/auth/callback"
	}

	frontendRedirect := os.Getenv("OAUTH_FRONTEND_REDIRECT")
	if frontendRedirect == "" {
		frontendRedirect = "http://localhost:4200"
	}

	cookieDomain := os.Getenv("APP_COOKIE_DOMAIN")
	if cookieDomain == "" {
		cookieDomain = "localhost"
	}

	cookieSameSite := os.Getenv("APP_COOKIE_SAME_SITE")
	if cookieSameSite == "" {
		cookieSameSite = "Lax"
	}

	geminiURL := os.Getenv("GEMINI_API_URL")
	if geminiURL == "" {
		geminiURL = "https://generativelanguage.googleapis.com/v1"
	}

	geminiModel := os.Getenv("GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = "gemini-2.5-flash"
	}

	return &Config{
		SQLiteCloudURL:      os.Getenv("DB_URL"),
		JwtSecret:           os.Getenv("JWT_SECRET"),
		GitHubClientID:      os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubClientSecret:  os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		GeminiAPIKey:        os.Getenv("GEMINI_API_KEY"),
		GeminiAPIURL:        geminiURL,
		GeminiModel:         geminiModel,
		GitHubRedirectURI:   gitHubRedirect,
		FrontendRedirectURI: frontendRedirect,
		CookieDomain:        cookieDomain,
		CookieSecure:        os.Getenv("APP_COOKIE_SECURE") == "true",
		CookieSameSite:      cookieSameSite,
		LogLevel:            defaultString(os.Getenv("LOG_LEVEL"), "info"),
	}
}

func defaultString(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
