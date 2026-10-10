package handler

import (
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
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
