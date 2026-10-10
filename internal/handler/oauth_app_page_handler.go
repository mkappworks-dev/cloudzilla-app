package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

// PageOAuthAuthorize renders the consent screen.
func (h *Handler) PageOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")

	app, err := h.Services.OAuthApp.GetByClientID(r.Context(), clientID)
	if err != nil {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}
	if !h.Services.OAuthApp.IsRedirectURIAllowed(app, redirectURI) {
		http.Error(w, "redirect_uri not allowed", http.StatusBadRequest)
		return
	}
	scopes, err := h.Services.OAuthApp.ParseScopes(r.Context(), r.URL.Query().Get("scope"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	claims, loggedIn := middleware.ClaimsFromContext(r.Context())
	if !loggedIn {
		h.Unauthorized(w, r)
		return
	}
	h.renderConsent(w, r, claims.UserID, http.StatusOK, view.OAuthAuthorizeData{
		App:         *app,
		Scopes:      scopes,
		RedirectURI: redirectURI,
		State:       r.URL.Query().Get("state"),
	})
}
