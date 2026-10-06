package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageLogin renders the login form page.
func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if _, ok := middleware.ClaimsFromContext(r.Context()); ok {
		http.Redirect(w, r, safeNextPath(next), http.StatusSeeOther)
		return
	}
	ldapEnabled, samlEnabled := h.ssoEnabled(r)
	h.render(w, r, pages.Login(view.LoginData{
		BasePage:          basePage(r, h.Services),
		LDAPEnabled:       ldapEnabled,
		SAMLEnabled:       samlEnabled,
		AllowRegistration: h.Services.SiteSetting.AllowRegistration(r.Context()),
		Next:              next,
	}))
}

// PageLoginSubmit handles form login, sets the auth cookie, and redirects on success.
func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	next := r.FormValue("next")
	ldapEnabled, samlEnabled := h.ssoEnabled(r)
	allowReg := h.Services.SiteSetting.AllowRegistration(r.Context())

	renderLoginError := func(msg string) {
		h.render(w, r, pages.Login(view.LoginData{
			BasePage:          basePage(r, h.Services),
			LDAPEnabled:       ldapEnabled,
			SAMLEnabled:       samlEnabled,
			AllowRegistration: allowReg,
			Error:             msg,
			Next:              next,
		}))
	}

	user, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if errors.Is(err, service.ErrAccountSuspended) {
		renderLoginError(accountSuspendedMessage)
		return
	}
	if err != nil {
		renderLoginError("Invalid credentials")
		return
	}

	if !user.IsSuperadmin && !user.IsInvited && !h.Services.SiteSetting.AllowLogin(r.Context()) {
		renderLoginError("Login is currently disabled")
		return
	}

	if err := h.signIn(w, r, user, token, next); err != nil {
		renderLoginError("Internal error")
	}
}

// accountSuspendedMessage is shown only once the first factor has passed, so it
// doesn't tell a stranger the account exists.
const accountSuspendedMessage = "This account is suspended. Contact your administrator."

// signIn finishes a sign-in whose first factor has passed. Every web sign-in
// route ends here so none of them can skip TOTP: a user who enabled it gets a
// short-lived pending cookie and is sent to /auth/2fa, and VerifyTOTP starts
// the session.
func (h *Handler) signIn(w http.ResponseWriter, r *http.Request, user *model.User, token, next string) error {
	return h.signInLinking(w, r, user, token, next, nil)
}

// signInLinking is signIn for an OAuth sign-in that also links the identity
// to the account. The link waits in the pending cookie until VerifyTOTP.
func (h *Handler) signInLinking(w http.ResponseWriter, r *http.Request, user *model.User, token, next string, link *service.OAuthLink) error {
	totpEnabled, _, err := h.Services.TOTP.GetUserTOTPState(r.Context(), user.ID)
	if err != nil {
		return err
	}
	if !totpEnabled {
		if link != nil {
			if err := h.completeOAuthLink(r, *link); err != nil {
				return err
			}
		}
		h.startSession(w, r, user, token, next)
		return nil
	}

	pendingToken, err := h.Services.TOTP.GeneratePendingToken(user.ID, h.Cfg.Auth.JWTSecret, link)
	if err != nil {
		return err
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
	http.Redirect(w, r, view.WithNext("/auth/2fa", next), http.StatusSeeOther)
	return nil
}

func (h *Handler) completeOAuthLink(r *http.Request, link service.OAuthLink) error {
	u, err := h.Services.OAuthLink.LinkByVerifiedEmail(r.Context(), link)
	if err != nil {
		return err
	}
	h.Services.AuditLog.Record(r.Context(), r, u.ID, u.Username, model.AuditActionOAuthConnect, model.AuditTargetUser, u.ID, u.Username,
		map[string]any{"provider": link.Provider, "oauth_id": link.ID, "email": link.Email, "via": "verified_email"})
	return nil
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, user *model.User, token, next string) {
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
	http.Redirect(w, r, safeNextPath(next), http.StatusSeeOther)
}

// safeNextPath returns the next= query value if it is a safe same-site path, else "/".
// Rejects schemed URLs, protocol-relative URLs, and non-rooted paths to prevent open redirects.
// Browsers read "/\host" as "//host" and strip tabs and newlines before parsing,
// so both are refused too. A backslash anywhere in the path is refused because
// http.Redirect cleans the path, turning "/./\host" into "/\host".
func safeNextPath(next string) string {
	nextPath, _, _ := strings.Cut(next, "?")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsRune(nextPath, '\\') ||
		strings.ContainsFunc(next, unicode.IsControl) {
		return "/"
	}
	return next
}
