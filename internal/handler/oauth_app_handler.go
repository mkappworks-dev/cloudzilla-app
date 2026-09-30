package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
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

// renderConsent fills in the confirmation fields the account needs.
func (h *Handler) renderConsent(w http.ResponseWriter, r *http.Request, userID int64, status int, data view.OAuthAuthorizeData) {
	u, err := h.Services.User.GetByID(r.Context(), userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data.BasePage = basePage(r, h.Services)
	data.HasPassword = u.PasswordHash != ""
	data.TOTPEnabled, _, _ = h.Services.TOTP.GetUserTOTPState(r.Context(), userID)
	// A framed consent page could be clickjacked into a one-click grant.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.render(w, r, pages.OAuthAuthorize(data))
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
	redir, err := url.Parse(redirectURI)
	if err != nil || !h.Services.OAuthApp.IsRedirectURIAllowed(app, redirectURI) {
		http.Error(w, "redirect_uri not allowed", http.StatusBadRequest)
		return
	}

	params := url.Values{}
	if r.FormValue("action") == "deny" {
		params.Set("error", "access_denied")
	} else {
		// A grant is a credential that outlives the session, so it needs the account's own factors.
		if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
			status, code, refused := reauthRefusal(claims.UserID, err)
			if !refused {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			h.renderConsent(w, r, claims.UserID, status, view.OAuthAuthorizeData{
				App:         *app,
				Scopes:      scopes,
				RedirectURI: redirectURI,
				State:       state,
				Error:       pages.SettingsErrorMessage(code),
			})
			return
		}
		code, err := h.Services.OAuthApp.Authorize(r.Context(), app.ID, claims.UserID, redirectURI, scopes, app)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		params.Set("code", code)
	}
	if state != "" {
		params.Set("state", state)
	}
	// Appended rather than merged, so the registered URI's own query is kept as-is (RFC 6749 §3.1.2).
	if redir.RawQuery != "" {
		redir.RawQuery += "&"
	}
	redir.RawQuery += params.Encode()
	http.Redirect(w, r, redir.String(), http.StatusSeeOther)
}

// TokenEndpoint handles POST /oauth/token (authorization_code grant). Failures get
// an RFC 6749 §5.2 error code and nothing else, so they reveal neither internals
// nor which client_ids exist.
func (h *Handler) TokenEndpoint(w http.ResponseWriter, r *http.Request) {
	// RFC 6749 §5.1; set on every response so no branch can forget it. Pragma is
	// omitted on purpose: RFC 9111 deprecates it.
	w.Header().Set("Cache-Control", "no-store")
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	switch r.FormValue("grant_type") {
	case "authorization_code":
	case "":
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	default:
		writeError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	clientID, clientSecret, usedBasic, err := tokenClientCredentials(r)
	code := r.FormValue("code")
	if err != nil || code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	token, err := h.Services.OAuthApp.ExchangeCode(r.Context(), clientID, clientSecret, code, r.FormValue("redirect_uri"))
	switch {
	case errors.Is(err, service.ErrInvalidClient):
		if usedBasic {
			w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
		}
		writeError(w, http.StatusUnauthorized, "invalid_client")
	case errors.Is(err, service.ErrInvalidGrant):
		writeError(w, http.StatusBadRequest, "invalid_grant")
	case err != nil:
		slog.Error("oauth token: exchange failed", "client_id", clientID, "error", err)
		writeError(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]string{
			"access_token": token,
			"token_type":   "bearer",
		})
	}
}

// tokenClientCredentials reads the client's credentials from an HTTP Basic header,
// whose parts RFC 6749 §2.3.1 form-encodes, or else from the form body. Sending a
// secret both ways is an error.
func tokenClientCredentials(r *http.Request) (clientID, clientSecret string, basic bool, err error) {
	user, pass, basic := r.BasicAuth()
	if !basic {
		return r.FormValue("client_id"), r.FormValue("client_secret"), false, nil
	}
	if clientID, err = url.QueryUnescape(user); err != nil {
		return "", "", true, err
	}
	if clientSecret, err = url.QueryUnescape(pass); err != nil {
		return "", "", true, err
	}
	if r.Form.Has("client_secret") || (r.Form.Has("client_id") && r.FormValue("client_id") != clientID) {
		return "", "", true, errors.New("client credentials in both the Authorization header and the body")
	}
	return clientID, clientSecret, true, nil
}

// CreateOAuthApp handles POST /api/oauth/apps.
func (h *Handler) CreateOAuthApp(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	isHTMX := r.Header.Get("HX-Request") == "true"
	var req struct {
		Name         string   `json:"name"`
		HomepageURL  string   `json:"homepage_url"`
		Description  string   `json:"description"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if isHTMX {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
		req.Name = strings.TrimSpace(r.FormValue("name"))
		req.HomepageURL = strings.TrimSpace(r.FormValue("homepage_url"))
		req.Description = r.FormValue("description")
		if uri := strings.TrimSpace(r.FormValue("redirect_uri")); uri != "" {
			req.RedirectURIs = []string{uri}
		}
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	app, rawSecret, err := h.Services.OAuthApp.CreateApp(r.Context(), claims.UserID, req.Name, req.HomepageURL, req.Description, req.RedirectURIs)
	if errors.Is(err, service.ErrInvalidRedirectURI) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create app")
		return
	}
	if isHTMX {
		apps, _ := h.Services.OAuthApp.ListByOwner(r.Context(), claims.UserID)
		w.Header().Set("Cache-Control", "no-store")
		h.render(w, r, fragments.OAuthAppsList(view.OAuthAppsFragData{
			Apps:            apps,
			NewClientID:     app.ClientID,
			NewClientSecret: rawSecret,
		}))
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
	if r.Header.Get("HX-Request") == "true" {
		apps, _ := h.Services.OAuthApp.ListByOwner(r.Context(), claims.UserID)
		h.render(w, r, fragments.OAuthAppsList(view.OAuthAppsFragData{Apps: apps}))
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
	if r.Header.Get("HX-Request") == "true" {
		auths, _ := h.Services.OAuthApp.ListAuthorizationsByUser(r.Context(), claims.UserID)
		h.render(w, r, fragments.OAuthAuthorizationsList(view.OAuthAuthorizationsFragData{Authorizations: auths}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
