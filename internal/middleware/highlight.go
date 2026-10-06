package middleware

import (
	"net/http"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
)

// HighlightBudget gives each request one highlight budget of maxBytes and
// maxTime, so a page of many Markdown bodies can't multiply the per-document cap.
func HighlightBudget(maxBytes int, maxTime time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := highlight.NewBudget(maxBytes, maxTime)
			next.ServeHTTP(w, r.WithContext(highlight.WithBudget(r.Context(), b)))
		})
	}
}
