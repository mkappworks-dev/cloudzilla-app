package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"golang.org/x/oauth2"
)

const settingsNoticeCookieName = "cz_settings_notice"

// ConnectGoogle handles POST /settings/connected-accounts/google (form: password, code).
// After re-authentication it sends the browser to Google with a state bound to this account.
func (h *Handler) ConnectGoogle(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.Cfg.OAuth.GoogleClientID == "" {
		redirectConnectedAccountsError(w, r, "google_not_configured")
		return
	}
	state, expiresAt, err := h.Services.OAuthLink.BeginLink(r.Context(), claims.UserID, r.FormValue("password"), r.FormValue("code"))
	if err != nil {
		connectedAccountsError(w, r, claims.UserID, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthLinkStateCookie,
		Value:    state,
		Path:     googleCallbackPath,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	// select_account makes the user pick the Google account to connect, instead of
	// Google silently using whichever one this browser is signed in to.
	authURL := h.googleOAuthConfig().AuthCodeURL(state, oauth2.SetAuthURLParam("prompt", "select_account"))
	// 303, not 307, so the browser does not re-send the password form to Google.
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// googleLinkCallback finishes ConnectGoogle: it links the Google account or refuses,
// and never creates an account or signs anyone in.
func (h *Handler) googleLinkCallback(w http.ResponseWriter, r *http.Request, state string) {
	http.SetCookie(w, &http.Cookie{Name: oauthLinkStateCookie, MaxAge: -1, Path: googleCallbackPath, HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure})

	// Spend the state before anything else can refuse, so no attempt leaves it redeemable.
	claims, signedIn := middleware.ClaimsFromContext(r.Context())
	grant, err := h.Services.OAuthLink.ConsumeLinkState(r.Context(), state, claims.UserID)
	switch {
	case !signedIn:
		redirectConnectedAccountsError(w, r, "google_link_signed_out")
		return
	case err != nil:
		connectedAccountsError(w, r, claims.UserID, err)
		return
	case r.URL.Query().Get("error") != "":
		redirectConnectedAccountsError(w, r, "google_link_cancelled")
		return
	}

	identity, _, err := h.googleIdentity(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		slog.Warn("google link: read google account", "user_id", claims.UserID, "error", err)
		redirectConnectedAccountsError(w, r, "google_link_failed")
		return
	}
	changed, err := h.Services.OAuthLink.Link(r.Context(), grant, identity)
	if err != nil {
		connectedAccountsError(w, r, claims.UserID, err)
		return
	}
	if changed {
		h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionOAuthConnect,
			model.AuditTargetUser, claims.UserID, claims.Username,
			map[string]any{"provider": googleProvider, "oauth_id": identity.ID, "email": identity.Email})
	}
	h.setSettingsFlash(w, settingsNoticeCookieName, "google_connected")
	http.Redirect(w, r, "/settings#connected-accounts", http.StatusSeeOther)
}

// DisconnectGoogle handles POST /settings/connected-accounts/google/disconnect (form: password, code).
func (h *Handler) DisconnectGoogle(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	oauthID, err := h.Services.OAuthLink.Unlink(r.Context(), claims.UserID, googleProvider, r.FormValue("password"), r.FormValue("code"))
	if err != nil {
		connectedAccountsError(w, r, claims.UserID, err)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionOAuthDisconnect,
		model.AuditTargetUser, claims.UserID, claims.Username, map[string]any{"provider": googleProvider, "oauth_id": oauthID})
	h.setSettingsFlash(w, settingsNoticeCookieName, "google_disconnected")
	http.Redirect(w, r, "/settings#connected-accounts", http.StatusSeeOther)
}

func connectedAccountsError(w http.ResponseWriter, r *http.Request, userID int64, err error) {
	code := "google_link_failed"
	switch {
	case errors.Is(err, service.ErrReauthFailed):
		code = "google_reauth_failed"
		// Nothing throttles these guesses, so leave a trail of them.
		slog.Warn("connected accounts: re-authentication failed", "user_id", userID)
	case errors.Is(err, service.ErrReauthNoPassword):
		code = "google_no_password"
	case errors.Is(err, service.ErrOAuthLinkInvalid):
		code = "google_link_invalid"
	case errors.Is(err, service.ErrOAuthLinkWrongUser):
		code = "google_link_wrong_user"
		slog.Warn("connected accounts: link state used by another account", "user_id", userID)
	case errors.Is(err, service.ErrOAuthEmailUnverified):
		code = "google_link_unverified"
	case errors.Is(err, service.ErrOAuthLinkedElsewhere):
		code = "google_link_taken"
		slog.Warn("connected accounts: Google account is linked elsewhere", "user_id", userID)
	case errors.Is(err, service.ErrOAuthAlreadyLinked):
		code = "google_already_connected"
	case errors.Is(err, service.ErrOAuthNotLinked):
		code = "google_not_connected"
	default:
		slog.Error("connected accounts", "user_id", userID, "error", err)
	}
	redirectConnectedAccountsError(w, r, code)
}

func redirectConnectedAccountsError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/settings?profile_error="+code+"#connected-accounts", http.StatusSeeOther)
}
