package middleware

import (
	"net/http"
	"runtime/debug"
	"time"

	"bragdev-go/internal/httpresp"
	"bragdev-go/internal/logger"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

// RequestLogger logs incoming requests, their latency and recovers from panics.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if rec := recover(); rec != nil {
				logger.Errorw("panic recovered", "panic", rec, "stack", string(debug.Stack()), "method", r.Method, "path", r.URL.Path)
				httpresp.JSONError(rw, http.StatusInternalServerError, "internal server error")
				return
			}
			logger.Infow("request completed", "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
		}()

		next.ServeHTTP(rw, r)
	})
}
