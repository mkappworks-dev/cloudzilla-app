package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
	"golang.org/x/oauth2"
)

const (
	// signInCodeCookie holds the one-time code a fresh provider sign-in left in
	// this browser; only this browser can then use it.
	signInCodeCookie      = "cz_reauth"
	signInFailedCookie    = "cz_reauth_failed"
	signInReturnCookie    = "cz_reauth_return"
	oauthReauthStateCookie = "oauth_reauth_state"
	samlReauthRelayPrefix  = "reauth:"
)

func confirmationFrom(r *http.Request) service.Confirmation {
	return withSignInCode(r, service.Confirmation{Password: r.FormValue("password"), Code: r.FormValue("code"), OneTimeCode: r.FormValue("email_code")})
}

// withSignInCode adds the code a provider sign-in left in this browser, unless
// the request carries an emailed code of its own.
func withSignInCode(r *http.Request, c service.Confirmation) service.Confirmation {
	if c.OneTimeCode == "" {
		if ck, err := r.Cookie(signInCodeCookie); err == nil {
			c.OneTimeCode = ck.Value
		}
	}
	return c
}

// reauthRefusal maps a failed confirmation to a status and a SettingsErrorMessage
// code, and reports whether err was one.
func reauthRefusal(userID int64, err error) (int, string, bool) {
	switch {
	case errors.Is(err, service.ErrReauthFailed):
		slog.Warn("re-authentication failed", "user_id", userID)
		return http.StatusForbidden, "reauth_failed", true
	case errors.Is(err, service.ErrReauthThrottled):
		slog.Warn("re-authentication throttled", "user_id", userID)
		return http.StatusTooManyRequests, "reauth_throttled", true
	case errors.Is(err, service.ErrReauthUnavailable):
		return http.StatusForbidden, "reauth_unavailable", true
	}
	return 0, "", false
}

// confirmFactors reports what userID confirms sensitive actions with. When that
// can't be read the form asks for both, since a factor the account lacks is ignored.
func (h *Handler) confirmFactors(r *http.Request, userID int64) components.ConfirmFactors {
	f, err := h.Services.Reauth.Factors(r.Context(), userID)
	if err != nil {
		slog.Error("load confirmation factors", "user_id", userID, "error", err)
		return components.ConfirmFactors{Password: true, Code: true}
	}
	cf := components.ConfirmFactors{
		Password:    f.Password,
		Code:        f.Code,
		Directory:   f.Directory,
		Provider:    f.Provider,
		Email:       f.Email,
		Unavailable: f.None(),
	}
	if ck, err := r.Cookie(signInCodeCookie); err == nil && ck.Value != "" {
		cf.ProviderReady = f.Provider != ""
	}
	if _, err := r.Cookie(signInFailedCookie); err == nil {
		cf.ProviderFailed = true
	}
	return cf
}

// confirmGrant is confirmAction for administering a repository or organization.
// A personal access token created with repo:admin skips it, so scripts can run:
// creating it took the password, it expires within 90 days, and each use is
// mailed to the owner. Every other credential confirms.
func (h *Handler) confirmGrant(w http.ResponseWriter, r *http.Request, userID int64, c service.Confirmation, slot string) bool {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok && claims.PAT && claims.Scoped && slices.Contains(claims.Scopes, model.ScopeRepoAdmin) {
		h.Services.Reauth.NotifyTokenAdmin(userID, claims.TokenName, r.Method+" "+r.URL.Path)
		return true
	}
	return h.confirmAction(w, r, userID, c, slot)
}

// confirmAction checks c before a sensitive action and answers the request when
// it fails. An HTMX form in a modal dialog names its error slot, where a toast
// would sit behind the backdrop; any other request gets a JSON error.
func (h *Handler) confirmAction(w http.ResponseWriter, r *http.Request, userID int64, c service.Confirmation, slot string) bool {
	_, err := h.Services.Reauth.Confirm(r.Context(), userID, withSignInCode(r, c))
	if err == nil {
		return true
	}
	status, code, refused := reauthRefusal(userID, err)
	if !refused {
		slog.Error("confirm sensitive action", "user_id", userID, "error", err)
		status, code = http.StatusInternalServerError, "reauth_error"
	}
	if slot != "" && r.Header.Get("HX-Request") == "true" {
		renderFormError(w, slot, pages.SettingsErrorMessage(code))
	} else {
		writeError(w, status, pages.SettingsErrorMessage(code))
	}
	return false
}

// redirectReauthRefusal sends a settings form whose confirmation failed back to
// the section at anchor with the matching error.
func redirectReauthRefusal(w http.ResponseWriter, r *http.Request, userID int64, err error, anchor string) {
	_, code, refused := reauthRefusal(userID, err)
	if !refused {
		slog.Error("confirm sensitive action", "user_id", userID, "error", err)
		code = "reauth_error"
	}
	http.Redirect(w, r, "/settings?profile_error="+code+"#"+anchor, http.StatusSeeOther)
}

// StartProviderSignIn handles POST /settings/reauth/{provider}: an account with
// no password or 2FA signs in again with Google or SAML to confirm a change.
// The provider sends the browser back to its callback, which hands this browser
// a one-time code and returns to return_to.
func (h *Handler) StartProviderSignIn(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	provider := chi.URLParam(r, "provider")
	returnTo := safeNextPath(r.FormValue("return_to"))
	state, expiresAt, err := h.Services.Reauth.BeginProviderSignIn(r.Context(), claims.UserID, provider)
	if err != nil {
		if !errors.Is(err, service.ErrSignInMismatch) {
			slog.Error("start provider sign-in", "user_id", claims.UserID, "error", err)
		}
		h.failProviderSignIn(w, r, returnTo)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: signInReturnCookie, Value: returnTo, Path: "/", Expires: expiresAt,
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	switch provider {
	case "google":
		http.SetCookie(w, &http.Cookie{
			Name: oauthReauthStateCookie, Value: state, Path: googleCallbackPath, Expires: expiresAt,
			HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
		})
		// max_age=0 asks Google to authenticate again rather than reuse its session.
		authURL := h.googleOAuthConfig().AuthCodeURL(state,
			oauth2.SetAuthURLParam("prompt", "select_account"), oauth2.SetAuthURLParam("max_age", "0"))
		http.Redirect(w, r, authURL, http.StatusSeeOther)
	case "saml":
		ssoURL, err := h.Services.SSO.SAMLAuthnRequestURL(r.Context(), samlReauthRelayPrefix+state, true)
		if err != nil {
			slog.Error("start saml sign-in", "user_id", claims.UserID, "error", err)
			h.failProviderSignIn(w, r, returnTo)
			return
		}
		http.Redirect(w, r, ssoURL, http.StatusSeeOther)
	}
}

// finishProviderSignIn gives this browser the one-time code and goes back to
// where the sign-in started.
func (h *Handler) finishProviderSignIn(w http.ResponseWriter, r *http.Request, code string) {
	http.SetCookie(w, &http.Cookie{
		Name: signInCodeCookie, Value: code, Path: "/", MaxAge: int((10 * time.Minute).Seconds()),
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{Name: signInFailedCookie, MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure})
	http.Redirect(w, r, h.signInReturnPath(w, r), http.StatusSeeOther)
}

func (h *Handler) failProviderSignIn(w http.ResponseWriter, r *http.Request, returnTo string) {
	http.SetCookie(w, &http.Cookie{
		Name: signInFailedCookie, Value: "1", Path: "/", MaxAge: 60,
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	if returnTo == "" {
		returnTo = h.signInReturnPath(w, r)
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func (h *Handler) signInReturnPath(w http.ResponseWriter, r *http.Request) string {
	http.SetCookie(w, &http.Cookie{Name: signInReturnCookie, MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure})
	if ck, err := r.Cookie(signInReturnCookie); err == nil {
		return safeNextPath(ck.Value)
	}
	return "/settings"
}

// SendConfirmCode handles POST /settings/confirm-code: it emails a one-time code
// to an account with no password or 2FA, for the confirm fields on any form.
func (h *Handler) SendConfirmCode(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	status, msg := http.StatusOK, "Code sent. It works once, for 10 minutes."
	switch err := h.Services.Reauth.SendEmailCode(r.Context(), claims.UserID); {
	case err == nil:
	case errors.Is(err, service.ErrEmailCodeCooldown):
		status, msg = http.StatusTooManyRequests, "A code was sent less than a minute ago. Check your email."
	case errors.Is(err, service.ErrEmailCodeNeedless):
		status, msg = http.StatusConflict, "This account confirms with its password or two-factor code."
	case errors.Is(err, service.ErrReauthUnavailable):
		status, msg = http.StatusConflict, pages.SettingsErrorMessage("reauth_unavailable")
	default:
		slog.Error("send confirmation code", "user_id", claims.UserID, "error", err)
		status, msg = http.StatusInternalServerError, "Couldn't send the code. Please try again."
	}
	// The status line swaps in whatever happened, so htmx always gets a 200.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(msg))
		return
	}
	if status != http.StatusOK {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// RevokeSessions handles POST /settings/sessions/revoke: it ends every session,
// then starts a new one for this browser. Tokens and OAuth grants are revoked separately.
func (h *Handler) RevokeSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := h.Services.User.RevokeSessions(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("revoke sessions", "user_id", claims.UserID, "error", err)
		http.Redirect(w, r, "/settings?profile_error=sessions_revoke_failed#sessions", http.StatusSeeOther)
		return
	}
	h.setAuthCookie(w, token)
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionSessionsRevoke, model.AuditTargetUser, claims.UserID, claims.Username, nil)
	http.Redirect(w, r, "/settings?sessions_revoked=1#sessions", http.StatusSeeOther)
}
