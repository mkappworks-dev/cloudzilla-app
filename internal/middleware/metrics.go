package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/metrics"
)

// Metrics records requests by chi route pattern, never by concrete path, to keep label cardinality bounded.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		method := metricsMethod(r.Method)
		metrics.HTTPInFlight.Inc()
		defer func() {
			metrics.HTTPInFlight.Dec()
			status := ww.Status()
			if rec := recover(); rec != nil {
				// Recoverer, further out, turns the panic into a 500.
				status = http.StatusInternalServerError
				defer panic(rec)
			}
			if status == 0 {
				status = http.StatusOK
			}
			route := "unmatched"
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if p := rctx.RoutePattern(); p != "" {
					route = p
				}
			}
			metrics.HTTPRequests.WithLabelValues(method, route, statusLabel(status)).Inc()
			metrics.HTTPDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(ww, r)
	})
}

func metricsMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "other"
}

func statusLabel(code int) string {
	if code < 100 || code > 599 {
		return "other"
	}
	return string([]byte{byte('0' + code/100), byte('0' + code/10%10), byte('0' + code%10)})
}
