package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageOAuthAuthorize renders the consent screen.
func (h *Handler) PageOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	scopeParam := r.URL.Query().Get("scope")
	scopes := strings.Fields(scopeParam)

	app, err := h.Services.OAuthApp.GetByClientID(r.Context(), clientID)
	if err != nil {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}

	_, loggedIn := middleware.ClaimsFromContext(r.Context())
	if !loggedIn {
		next := r.URL.RequestURI()
		if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
			next = "/"
		}
		http.Redirect(w, r, "/login?next="+next, http.StatusSeeOther)
		return
	}

	h.render(w, r, pages.OAuthAuthorize(view.OAuthAuthorizeData{
		BasePage:    basePage(r, h.Services),
		App:         *app,
		Scopes:      scopes,
		RedirectURI: redirectURI,
		State:       r.URL.Query().Get("state"),
	}))
}

// ConfirmAuthorize handles the POST from the consent form.
func (h *Handler) ConfirmAuthorize(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	scopeParam := r.FormValue("scope")
	scopes := strings.Fields(scopeParam)

	// Fetch and validate the app before using redirectURI in any redirect.
	// This prevents open-redirect attacks on the deny path.
	app, err := h.Services.OAuthApp.GetByClientID(r.Context(), clientID)
	if err != nil {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}
	if !h.Services.OAuthApp.IsRedirectURIAllowed(app, redirectURI) {
		http.Error(w, "redirect_uri not allowed", http.StatusBadRequest)
		return
	}

	if r.FormValue("action") == "deny" {
		redir := redirectURI + "?error=access_denied"
		if state != "" {
			redir += "&state=" + state
		}
		http.Redirect(w, r, redir, http.StatusSeeOther)
		return
	}

	code, err := h.Services.OAuthApp.Authorize(r.Context(), app.ID, claims.UserID, redirectURI, scopes, app)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	redir := redirectURI + "?code=" + code
	if state != "" {
		redir += "&state=" + state
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

// TokenEndpoint handles POST /oauth/token (authorization_code grant).
func (h *Handler) TokenEndpoint(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	grantType := r.FormValue("grant_type")
	if grantType != "authorization_code" {
		writeError(w, http.StatusBadRequest, "unsupported grant_type")
		return
	}
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")
	code := r.FormValue("code")

	token, err := h.Services.OAuthApp.ExchangeCode(r.Context(), clientID, clientSecret, code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": token,
		"token_type":   "bearer",
	})
}

// PageOAuthApps renders the user's registered apps and granted authorizations.
func (h *Handler) PageOAuthApps(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	apps, err := h.Services.OAuthApp.ListByOwner(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	auths, err := h.Services.OAuthApp.ListAuthorizationsByUser(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, pages.OAuthApps(view.OAuthAppsData{
		BasePage:       basePage(r, h.Services),
		Apps:           apps,
		Authorizations: auths,
	}))
}

// CreateOAuthApp handles POST /api/oauth/apps.
func (h *Handler) CreateOAuthApp(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Name         string   `json:"name"`
		HomepageURL  string   `json:"homepage_url"`
		Description  string   `json:"description"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	app, rawSecret, err := h.Services.OAuthApp.CreateApp(r.Context(), claims.UserID, req.Name, req.HomepageURL, req.Description, req.RedirectURIs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create app")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"app":           app,
		"client_secret": rawSecret,
	})
}

// DeleteOAuthApp handles DELETE /api/oauth/apps/{id}.
func (h *Handler) DeleteOAuthApp(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.Services.OAuthApp.DeleteApp(r.Context(), id, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete app")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevokeOAuthAuthorization handles DELETE /api/oauth/authorizations/{id}.
func (h *Handler) RevokeOAuthAuthorization(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.Services.OAuthApp.RevokeAccess(r.Context(), id, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke authorization")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
