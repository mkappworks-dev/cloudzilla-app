package handler

import (
	"net/http"
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

// Unauthorized redirects HTML requests to /login (preserving the original path)
// and returns a JSON 401 to API requests.
func (h *Handler) Unauthorized(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	http.Redirect(w, r, view.WithNext("/login", safeNextPath(r.URL.RequestURI())), http.StatusSeeOther)
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
