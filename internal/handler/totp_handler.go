package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const totpPendingCookieName = "cz_totp_pending"

// SetupTOTP handles POST /settings/security/setup.
func (h *Handler) SetupTOTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	secret, _, err := h.Services.TOTP.Generate(claims.Username, "Cloudzilla")
	if err != nil {
		http.Redirect(w, r, "/settings?profile_error=totp_setup_failed#security", http.StatusSeeOther)
		return
	}
	if err := h.Services.TOTP.StoreSecret(r.Context(), claims.UserID, secret); err != nil {
		http.Redirect(w, r, "/settings?profile_error=totp_setup_failed#security", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings#security", http.StatusSeeOther)
}

// EnableTOTP handles POST /api/user/totp/enable (form: secret, code). On success
// the freshly-generated backup codes are stashed in a short-lived cookie so the
// settings page can show them exactly once.
func (h *Handler) EnableTOTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	secret := r.FormValue("secret")
	code := r.FormValue("code")
	if secret == "" || code == "" {
		refuseSettingsForm(w, r, totpEnableFormError, "security", "totp_missing_fields")
		return
	}
	// A code enrolled from a stolen session would lock the owner out at their next sign-in.
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, withSignInCode(r, service.Confirmation{Password: r.FormValue("password"), OneTimeCode: r.FormValue("email_code")})); err != nil {
		redirectReauthRefusal(w, r, claims.UserID, err, totpEnableFormError, "security")
		return
	}

	rawCodes, err := h.Services.TOTP.Enable(r.Context(), claims.UserID, secret, code)
	if err != nil {
		refuseSettingsForm(w, r, totpEnableFormError, "security", "totp_invalid_code")
		return
	}

	h.setSettingsFlash(w, backupCodesCookieName, strings.Join(rawCodes, ","))
	redirectAfterSave(w, r, "/settings#security")
}

const (
	totpEnableFormError  = "#totp-enable-form-error"
	totpDisableFormError = "#totp-disable-form-error"
)

// DisableTOTP handles POST /api/user/totp/disable (form: code).
func (h *Handler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	code := r.FormValue("code")
	if code == "" {
		refuseSettingsForm(w, r, totpDisableFormError, "security", "totp_missing_code")
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		redirectReauthRefusal(w, r, claims.UserID, err, totpDisableFormError, "security")
		return
	}

	if err := h.Services.TOTP.Disable(r.Context(), claims.UserID, code); err != nil {
		refuseSettingsForm(w, r, totpDisableFormError, "security", "totp_invalid_code")
		return
	}
	redirectAfterSave(w, r, "/settings#security")
}

// PageTOTPVerify renders GET /auth/2fa — the 6-digit input page.
func (h *Handler) PageTOTPVerify(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if _, err := r.Cookie(totpPendingCookieName); err != nil {
		http.Redirect(w, r, view.WithNext("/login", next), http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{BasePage: basePage(r, h.Services), Next: next}))
}

// VerifyTOTP handles POST /auth/2fa/verify (form: code, backup_code).
func (h *Handler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	next := r.FormValue("next")
	loginURL := view.WithNext("/login", next)
	pendingCookie, err := r.Cookie(totpPendingCookieName)
	if err != nil {
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}

	token, err := jwt.Parse(pendingCookie.Value, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.Cfg.Auth.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		http.SetCookie(w, &http.Cookie{
			Name: totpPendingCookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure,
		})
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}
	userID := int64(mapClaims["sub"].(float64))

	u, err := h.Services.TOTP.StoreGetUser(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}

	if err := h.Services.Reauth.CheckSecondFactor(r.Context(), userID, r.FormValue("code"), r.FormValue("backup_code")); err != nil {
		msg := "Invalid code. Please try again."
		switch {
		case errors.Is(err, service.ErrReauthThrottled):
			msg = "Too many incorrect codes. Try again in 15 minutes."
			slog.Warn("two-factor sign-in throttled", "user_id", userID)
		case !errors.Is(err, service.ErrReauthFailed):
			slog.Error("two-factor sign-in", "user_id", userID, "error", err)
			msg = "Something went wrong. Please try again."
		}
		h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{
			BasePage: basePage(r, h.Services),
			Error:    msg,
			Next:     next,
		}))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: totpPendingCookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure,
	})
	// Minted before the link completes, so a suspended account changes nothing.
	fullToken, err := h.Services.User.GenerateTokenForUser(r.Context(), userID)
	if errors.Is(err, service.ErrAccountSuspended) {
		h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{
			BasePage: basePage(r, h.Services),
			Error:    accountSuspendedMessage,
			Next:     next,
		}))
		return
	}
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	// The OAuth identity was the first factor only because it matched this
	// account's address; if the link no longer holds, neither does the sign-in.
	if link := h.Services.TOTP.PendingOAuthLink(mapClaims, userID); link != nil {
		if err := h.completeOAuthLink(r, *link); err != nil {
			if !errors.Is(err, service.ErrOAuthAccountExists) {
				slog.Error("complete oauth link after totp", "user_id", userID, "error", err)
			}
			h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{
				BasePage: basePage(r, h.Services),
				Error:    "Your account changed while you were signing in with Google. Sign in again.",
				Next:     next,
			}))
			return
		}
	}

	h.startSession(w, r, u, fullToken, next)
}
