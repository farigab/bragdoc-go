package middleware

import (
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"bragdev-go/internal/config"
)

// CORSMiddleware returns a middleware that sets CORS headers based on config.
func CORSMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	allowedOrigins := buildAllowedOrigins(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && len(allowedOrigins) == 0 {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}

			setOriginHeaders(w, origin, allowedOrigins)
			setAllowHeaders(w, r)

			if r.Method == http.MethodOptions {
				handlePreflight(w, origin, allowedOrigins)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// buildAllowedOrigins resolves the allowed origins from config, falling back
// to the APP_ALLOWED_ORIGINS environment variable when config yields nothing.
func buildAllowedOrigins(cfg *config.Config) []string {
	if cfg != nil && cfg.FrontendRedirectURI != "" {
		if origins := parseOrigins(cfg.FrontendRedirectURI); len(origins) > 0 {
			return origins
		}
	}
	return parseOrigins(os.Getenv("APP_ALLOWED_ORIGINS"))
}

// parseOrigins splits a comma-separated list of URLs and normalises each entry
// to scheme://host. Blank entries are ignored; unparseable entries are kept as-is.
func parseOrigins(raw string) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})

	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if u, err := url.Parse(p); err == nil && u.Scheme != "" && u.Host != "" {
			p = u.Scheme + "://" + u.Host
		}

		if _, ok := seen[p]; ok {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// isOriginAllowed reports whether origin appears in the allowed list.
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	return slices.Contains(allowedOrigins, origin)
}

// setOriginHeaders writes Access-Control-Allow-Origin (and related headers)
// based on the configured allowed origins.
func setOriginHeaders(w http.ResponseWriter, origin string, allowedOrigins []string) {
	if len(allowedOrigins) == 0 {
		// No whitelist configured: do not set CORS headers.
		return
	}
	if origin != "" && isOriginAllowed(origin, allowedOrigins) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

// setAllowHeaders writes the Access-Control-Allow-Methods and
// Access-Control-Allow-Headers response headers.
func setAllowHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	// Echo requested headers back so custom headers like x-health-check are permitted.
	if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
		w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
	} else {
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Authorization, X-Requested-With, X-Health-Check")
	}
}

// handlePreflight responds to an OPTIONS preflight request. It rejects the
// request when a specific origin whitelist is configured and the request origin
// is not on it; otherwise it responds with 204 No Content.
func handlePreflight(w http.ResponseWriter, origin string, allowedOrigins []string) {
	if origin != "" && len(allowedOrigins) == 0 {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if len(allowedOrigins) > 0 && origin != "" && !isOriginAllowed(origin, allowedOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
