package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Logger returns middleware that logs each request's method, path, status, and duration.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("request",
			"method", r.Method,
			"path", loggedPath(r),
			"status", rw.status,
			"duration", time.Since(start),
			"remote", r.RemoteAddr,
		)
	})
}

// Invite and signup link tokens are credentials, so their routes log the
// pattern. The pattern is only complete once routing has run.
func loggedPath(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); strings.Contains(pattern, "{token}") {
			return pattern
		}
	}
	return r.URL.Path
}
