package handler

import (
	"net/http"
	"time"

	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageSSOSettings renders GET /admin/sso — superadmin only.
func (h *Handler) PageSSOSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ldapCfg, _ := h.Services.SSO.GetConfig(r.Context(), "ldap")
	samlCfg, _ := h.Services.SSO.GetConfig(r.Context(), "saml")

	h.render(w, r, pages.SSOSettings(view.SSOSettingsData{
		BasePage:   basePage(r, h.Services),
		LDAPConfig: ldapCfg,
		SAMLConfig: samlCfg,
	}))
}

// SaveSSOConfig handles POST /admin/sso — superadmin only.
func (h *Handler) SaveSSOConfig(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	provider := r.FormValue("provider")
	enabled := r.FormValue("enabled") == "true"

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
			model.SAMLKeyACSURL:      r.FormValue("saml_acs_url"),
			model.SAMLKeyCert:        r.FormValue("saml_idp_cert"),
		}
	default:
		ldapCfg, _ := h.Services.SSO.GetConfig(r.Context(), "ldap")
		samlCfg, _ := h.Services.SSO.GetConfig(r.Context(), "saml")
		h.render(w, r, pages.SSOSettings(view.SSOSettingsData{
			BasePage:   basePage(r, h.Services),
			LDAPConfig: ldapCfg,
			SAMLConfig: samlCfg,
			Error:      "Unknown provider: " + provider,
		}))
		return
	}

	if err := h.Services.SSO.SetConfig(r.Context(), provider, cfg, enabled); err != nil {
		ldapCfg, _ := h.Services.SSO.GetConfig(r.Context(), "ldap")
		samlCfg, _ := h.Services.SSO.GetConfig(r.Context(), "saml")
		h.render(w, r, pages.SSOSettings(view.SSOSettingsData{
			BasePage:   basePage(r, h.Services),
			LDAPConfig: ldapCfg,
			SAMLConfig: samlCfg,
			Error:      "Failed to save SSO config: " + err.Error(),
		}))
		return
	}

	ldapCfg, _ := h.Services.SSO.GetConfig(r.Context(), "ldap")
	samlCfg, _ := h.Services.SSO.GetConfig(r.Context(), "saml")
	h.render(w, r, pages.SSOSettings(view.SSOSettingsData{
		BasePage:   basePage(r, h.Services),
		LDAPConfig: ldapCfg,
		SAMLConfig: samlCfg,
		Success:    "SSO configuration saved.",
	}))
}

// LDAPLogin handles POST /auth/ldap — accepts form fields username + password.
func (h *Handler) LDAPLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: true,
			Error:       "Username and password are required.",
		}))
		return
	}

	user, token, err := h.Services.SSO.AuthenticateLDAP(r.Context(), username, password)
	if err != nil {
		ldapEnabled, samlEnabled := h.ssoEnabled(r)
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       "LDAP authentication failed. Please check your credentials.",
		}))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
	_ = user
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// InitiateSAML handles GET /auth/saml — redirects to the IdP SSO URL.
func (h *Handler) InitiateSAML(w http.ResponseWriter, r *http.Request) {
	ssoURL, err := h.Services.SSO.SAMLAuthnRequestURL(r.Context())
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

	user, token, err := h.Services.SSO.HandleSAMLCallback(r.Context(), samlResponse)
	if err != nil {
		ldapEnabled, samlEnabled := h.ssoEnabled(r)
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       "SAML authentication failed: " + err.Error(),
		}))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
	_ = user
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
