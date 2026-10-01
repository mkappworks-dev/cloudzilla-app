package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageSSOSettings renders GET /admin/sso — superadmin only.
func (h *Handler) PageSSOSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ldapCfg, err := h.Services.SSO.GetConfig(r.Context(), "ldap")
	if err != nil {
		slog.Error("failed to load ldap sso config", "error", err)
		http.Error(w, "failed to load SSO configuration", http.StatusInternalServerError)
		return
	}
	samlCfg, err := h.Services.SSO.GetConfig(r.Context(), "saml")
	if err != nil {
		slog.Error("failed to load saml sso config", "error", err)
		http.Error(w, "failed to load SSO configuration", http.StatusInternalServerError)
		return
	}

	h.render(w, r, pages.SSOSettings(view.SSOSettingsData{
		BasePage:   basePage(r, h.Services),
		LDAPConfig: ldapCfg,
		SAMLConfig: samlCfg,
		Confirm:    h.confirmFactors(r, claims.UserID),
	}))
}

// renderSSOSettings re-renders the settings page after a save, with the outcome.
func (h *Handler) renderSSOSettings(w http.ResponseWriter, r *http.Request, userID int64, errMsg, success string) {
	ldapCfg, ldapErr := h.Services.SSO.GetConfig(r.Context(), "ldap")
	if ldapErr != nil {
		slog.Error("failed to reload ldap sso config", "error", ldapErr)
	}
	samlCfg, samlErr := h.Services.SSO.GetConfig(r.Context(), "saml")
	if samlErr != nil {
		slog.Error("failed to reload saml sso config", "error", samlErr)
	}
	h.render(w, r, pages.SSOSettings(view.SSOSettingsData{
		BasePage:   basePage(r, h.Services),
		LDAPConfig: ldapCfg,
		SAMLConfig: samlCfg,
		Error:      errMsg,
		Success:    success,
		Confirm:    h.confirmFactors(r, userID),
	}))
}

// SaveSSOConfig handles POST /admin/sso — superadmin only. A directory or IdP
// the session's holder controls could sign in as its users, so it's confirmed.
func (h *Handler) SaveSSOConfig(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		_, code, refused := reauthRefusal(claims.UserID, err)
		if !refused {
			slog.Error("confirm sso change", "user_id", claims.UserID, "error", err)
			code = "reauth_error"
		}
		h.renderSSOSettings(w, r, claims.UserID, pages.SettingsErrorMessage(code), "")
		return
	}

	provider := r.FormValue("provider")
	// HTML checkboxes submit the field only when checked (value may be "on",
	// "true", or any custom value). Presence means enabled; absence means false.
	enabled := r.FormValue("enabled") != ""

	var cfg map[string]string
	switch provider {
	case "ldap":
		cfg = map[string]string{
			model.LDAPKeyHost:       r.FormValue("ldap_host"),
			model.LDAPKeyPort:       r.FormValue("ldap_port"),
			model.LDAPKeyBaseDN:     r.FormValue("ldap_base_dn"),
			model.LDAPKeyBindDNTmpl: r.FormValue("ldap_bind_dn_tmpl"),
			model.LDAPKeyUseTLS:     r.FormValue("ldap_use_tls"),
		}
	case "saml":
		cfg = map[string]string{
			model.SAMLKeyEntityID:    r.FormValue("saml_entity_id"),
			model.SAMLKeyMetadataURL: r.FormValue("saml_metadata_url"),
			model.SAMLKeySSOURL:      r.FormValue("saml_sso_url"),
			model.SAMLKeyACSURL:      r.FormValue("saml_acs_url"),
			model.SAMLKeyCert:        r.FormValue("saml_idp_cert"),
		}
	default:
		h.renderSSOSettings(w, r, claims.UserID, "Unknown provider: "+provider, "")
		return
	}

	if err := h.Services.SSO.SetConfig(r.Context(), provider, cfg, enabled); err != nil {
		slog.Error("save sso config failed", "provider", provider, "error", err)
		h.renderSSOSettings(w, r, claims.UserID, "Could not save the SSO configuration. Check the server logs.", "")
		return
	}
	h.renderSSOSettings(w, r, claims.UserID, "", "SSO configuration saved.")
}

// LDAPLogin handles POST /auth/ldap — accepts form fields username + password.
func (h *Handler) LDAPLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	next := r.FormValue("next")

	if username == "" || password == "" {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: true,
			Error:       "Username and password are required.",
			Next:        next,
		}))
		return
	}

	renderLoginError := func(msg string) {
		ldapEnabled, samlEnabled := h.ssoEnabled(r)
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       msg,
			Next:        next,
		}))
	}

	user, token, err := h.Services.SSO.AuthenticateLDAP(r.Context(), username, password)
	if err != nil {
		renderLoginError("LDAP authentication failed. Please check your credentials.")
		return
	}

	if err := h.signIn(w, r, user, token, next); err != nil {
		slog.Error("ldap sign-in failed", "error", err)
		renderLoginError("Internal error")
	}
}

// InitiateSAML handles GET /auth/saml — redirects to the IdP SSO URL.
func (h *Handler) InitiateSAML(w http.ResponseWriter, r *http.Request) {
	ssoURL, err := h.Services.SSO.SAMLAuthnRequestURL(r.Context(), r.URL.Query().Get("next"), false)
	if err != nil {
		http.Error(w, "SAML not configured", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, ssoURL, http.StatusFound)
}

// SAMLCallback handles POST /auth/saml/callback — receives the IdP response.
func (h *Handler) SAMLCallback(w http.ResponseWriter, r *http.Request) {
	samlResponse := r.FormValue("SAMLResponse")
	if samlResponse == "" {
		http.Error(w, "missing SAMLResponse", http.StatusBadRequest)
		return
	}

	if state, ok := strings.CutPrefix(r.FormValue("RelayState"), samlReauthRelayPrefix); ok {
		h.samlReauthCallback(w, r, samlResponse, state)
		return
	}

	user, token, err := h.Services.SSO.HandleSAMLCallback(r.Context(), samlResponse)
	if err != nil {
		slog.Error("saml callback failed", "error", err)
		ldapEnabled, samlEnabled := h.ssoEnabled(r)
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       "SAML authentication failed.",
			Next:        r.FormValue("RelayState"),
		}))
		return
	}

	if err := h.signIn(w, r, user, token, r.FormValue("RelayState")); err != nil {
		slog.Error("saml sign-in failed", "error", err)
		http.Error(w, "failed to sign in", http.StatusInternalServerError)
	}
}

// samlReauthCallback finishes StartProviderSignIn for SAML. The IdP posts here
// cross-site, so the session cookie isn't sent; the state names the account,
// and the code goes to whichever browser the IdP authenticated as that account.
func (h *Handler) samlReauthCallback(w http.ResponseWriter, r *http.Request, samlResponse, state string) {
	user, _, err := h.Services.SSO.HandleSAMLCallback(r.Context(), samlResponse)
	if err != nil {
		slog.Warn("saml sign-in to confirm", "error", err)
		h.failProviderSignIn(w, r, "")
		return
	}
	_, code, err := h.Services.Reauth.FinishSAMLSignIn(r.Context(), state, user.ID)
	if err != nil {
		if !errors.Is(err, service.ErrSignInMismatch) {
			slog.Error("saml sign-in to confirm", "user_id", user.ID, "error", err)
		}
		h.failProviderSignIn(w, r, "")
		return
	}
	h.finishProviderSignIn(w, r, code)
}

// SAMLMetadata handles GET /auth/saml/metadata — serves SP metadata XML.
func (h *Handler) SAMLMetadata(w http.ResponseWriter, r *http.Request) {
	xmlStr, err := h.Services.SSO.SAMLMetadataXML(r.Context())
	if err != nil {
		http.Error(w, "SAML not configured", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xmlStr))
}

// ssoEnabled returns whether LDAP and SAML are currently enabled.
func (h *Handler) ssoEnabled(r *http.Request) (ldap, saml bool) {
	if cfg, err := h.Services.SSO.GetConfig(r.Context(), "ldap"); err == nil && cfg != nil {
		ldap = cfg.Enabled
	}
	if cfg, err := h.Services.SSO.GetConfig(r.Context(), "saml"); err == nil && cfg != nil {
		saml = cfg.Enabled
	}
	return
}
