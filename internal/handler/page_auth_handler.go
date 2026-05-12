package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageLogin renders the login form page.
func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ldapEnabled, samlEnabled := h.ssoEnabled(r)
	h.render(w, r, pages.Login(view.LoginData{
		BasePage:          basePage(r, h.Services),
		LDAPEnabled:       ldapEnabled,
		SAMLEnabled:       samlEnabled,
		AllowRegistration: h.Services.SiteSetting.AllowRegistration(r.Context()),
	}))
}

// PageLoginSubmit handles form login, sets the auth cookie, and redirects on success.
func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	ldapEnabled, samlEnabled := h.ssoEnabled(r)
	allowReg := h.Services.SiteSetting.AllowRegistration(r.Context())

	renderLoginError := func(msg string) {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:          basePage(r, h.Services),
			LDAPEnabled:       ldapEnabled,
			SAMLEnabled:       samlEnabled,
			AllowRegistration: allowReg,
			Error:             msg,
		}))
	}

	user, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		renderLoginError("Invalid credentials")
		return
	}

	if !user.IsSuperadmin && !user.IsInvited && !h.Services.SiteSetting.AllowLogin(r.Context()) {
		renderLoginError("Login is currently disabled")
		return
	}

	totpEnabled, _, err := h.Services.TOTP.GetUserTOTPState(r.Context(), user.ID)
	if err != nil {
		renderLoginError("Internal error")
		return
	}

	if totpEnabled {
		pendingToken, err := h.Services.TOTP.GeneratePendingToken(user.ID, h.Cfg.Auth.JWTSecret)
		if err != nil {
			renderLoginError("Internal error")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     totpPendingCookieName,
			Value:    pendingToken,
			HttpOnly: true,
			Secure:   h.Cfg.Auth.CookieSecure,
			Path:     "/",
			Expires:  time.Now().Add(5 * time.Minute),
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/auth/2fa", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})

	h.Services.AuditLog.Record(r.Context(), r, user.ID, user.Username, model.AuditActionLogin, "user", user.ID, user.Username, nil)

	http.Redirect(w, r, safeNextPath(r.URL.Query().Get("next")), http.StatusSeeOther)
}

// safeNextPath returns the next= query value if it is a safe same-site path, else "/".
// Rejects schemed URLs, protocol-relative URLs, and non-rooted paths to prevent open redirects.
func safeNextPath(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}
