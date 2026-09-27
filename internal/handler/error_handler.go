package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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
	next := r.URL.RequestURI()
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
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

// createFormErrorMessage returns page text for the create-form errors a user
// can act on, or "" when err is internal: store and driver errors carry
// constraint names and SQLSTATEs, so callers log them and show a generic message.
func createFormErrorMessage(err error) string {
	if errors.Is(err, service.ErrTitleTooLong) {
		return fmt.Sprintf("Title is too long (maximum %d characters)", service.MaxTitleLen)
	}
	return ""
}

// passwordLengthMessage returns page text for a password bcrypt can't hash, or
// "" when its length is fine.
func passwordLengthMessage(password string) string {
	if len(password) > service.MaxPasswordBytes {
		return fmt.Sprintf("Password is too long (maximum %d bytes)", service.MaxPasswordBytes)
	}
	return ""
}
