package handler

import (
	"net/http"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	ldapEnabled, samlEnabled := h.ssoEnabled(r)
	h.render(w, r, pages.Login(view.LoginData{
		BasePage:    basePage(r, h.Services),
		LDAPEnabled: ldapEnabled,
		SAMLEnabled: samlEnabled,
	}))
}

func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	ldapEnabled, samlEnabled := h.ssoEnabled(r)

	user, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       "Invalid credentials",
		}))
		return
	}

	if !user.IsSuperadmin && !user.IsInvited && !h.Services.SiteSetting.AllowLogin(r.Context()) {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       "Login is currently disabled",
		}))
		return
	}

	totpEnabled, _, err := h.Services.TOTP.GetUserTOTPState(r.Context(), user.ID)
	if err != nil {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:    basePage(r, h.Services),
			LDAPEnabled: ldapEnabled,
			SAMLEnabled: samlEnabled,
			Error:       "Internal error",
		}))
		return
	}

	if totpEnabled {
		pendingToken, err := h.Services.TOTP.GeneratePendingToken(user.ID, h.Cfg.Auth.JWTSecret)
		if err != nil {
			h.render(w, r, pages.Login(view.LoginData{BasePage: basePage(r, h.Services), Error: "Internal error"}))
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

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
