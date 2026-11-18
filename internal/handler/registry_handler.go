package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const registryVersionHeader = "Docker-Distribution-API-Version"

// RegistryToken is GET /v2/token: docker sends a personal access token as the Basic password, or nothing.
func (h *Handler) RegistryToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(registryVersionHeader, "registry/2.0")
	var requested []service.RegistryScope
	for _, s := range r.URL.Query()["scope"] {
		if scope, ok := service.ParseRegistryScope(s); ok {
			requested = append(requested, scope)
		}
	}
	_, pat, sent := r.BasicAuth()
	if sent && pat == "" {
		pat = "-" // an empty password is a failed login, not an anonymous request
	}

	tok, err := h.Services.Registry.IssueToken(r.Context(), pat, requested)
	var forbidden *service.RegistryForbiddenError
	switch {
	case errors.As(err, &forbidden):
		middleware.WriteRegistryError(w, http.StatusForbidden, "DENIED", forbidden.Reason, nil)
	case errors.Is(err, service.ErrRegistryCredentials):
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		middleware.WriteRegistryError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
	case err != nil:
		middleware.WriteRegistryError(w, http.StatusInternalServerError, "UNSUPPORTED", "could not issue a token", nil)
	default:
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{
			"token":        tok.Token,
			"access_token": tok.Token,
			"expires_in":   tok.ExpiresIn,
			"issued_at":    tok.IssuedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
}

// Registry serves every other /v2 path once RegistryAuth has admitted it. Operations land here as tickets build them.
func (h *Handler) Registry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(registryVersionHeader, "registry/2.0")
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/v2"), "/")
	if rest == "" {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	route, ok := middleware.RegistryRouteFromContext(r.Context())
	if !ok {
		middleware.WriteRegistryError(w, http.StatusNotFound, "UNSUPPORTED", "no such registry operation", nil)
		return
	}
	if h.registryBlobOp(w, r, route) || h.registryManifestOp(w, r, route) {
		return
	}
	middleware.WriteRegistryError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "this registry operation is not available yet",
		map[string]string{"name": route.Name, "operation": string(route.Op)})
}

// RegistryDisabled answers the whole /v2 tree when registry.enabled is false.
func (h *Handler) RegistryDisabled(w http.ResponseWriter, _ *http.Request) {
	middleware.WriteRegistryError(w, http.StatusNotFound, "UNSUPPORTED", "the registry is disabled", nil)
}
