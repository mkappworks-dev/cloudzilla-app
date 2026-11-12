package handler

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// isAPIRequest reports whether a request targets the JSON API surface or the
// HTML site. API requests get JSON error bodies; HTML requests get branded pages
// or login redirects.
func isAPIRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/")
}

// NotFound renders the branded 404 page for HTML or a JSON error for API requests.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNotFound)
	h.render(w, r, pages.NotFound(basePage(r, h.Services)))
}

// ServerError renders the branded 500 page for HTML or a JSON error for API requests.
func (h *Handler) ServerError(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	h.render(w, r, pages.ServerError(basePage(r, h.Services)))
}

// Recoverer turns a handler panic into the branded 500. http.ErrAbortHandler passes through: net/http uses it to
// abort a response on purpose.
func (h *Handler) Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			slog.Error("handler panic", "error", rec, "method", r.Method, "path", r.URL.Path, "stack", string(debug.Stack()))
			if r.Header.Get("Upgrade") == "websocket" {
				return
			}
			h.ServerError(w, r)
		}()
		next.ServeHTTP(w, r)
	})
}

// Unauthorized redirects HTML requests to /login (preserving the original path)
// and returns a JSON 401 to API requests.
func (h *Handler) Unauthorized(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	http.Redirect(w, r, view.WithNext("/login", safeNextPath(r.URL.RequestURI())), http.StatusSeeOther)
}

// RateLimited answers a request over its rate limit: JSON for the API, plain
// text otherwise, which git shows to the user.
func (h *Handler) RateLimited(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	http.Error(w, "Rate limit exceeded. Try again after the time in the Retry-After header.", http.StatusTooManyRequests)
}

// Forbidden renders the branded 403 page for HTML or a JSON error for API requests.
func (h *Handler) Forbidden(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	w.WriteHeader(http.StatusForbidden)
	h.render(w, r, pages.Forbidden(basePage(r, h.Services)))
}
