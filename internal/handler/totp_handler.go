package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const totpPendingCookieName = "cz_totp_pending"

// SetupTOTP handles POST /settings/security/setup. Generates a TOTP secret,
// stores it as pending, and redirects back to /settings#security where the
// QR code + verify form is rendered inline by the consolidated settings page.
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
		http.Redirect(w, r, "/settings?profile_error=totp_missing_fields#security", http.StatusSeeOther)
		return
	}

	rawCodes, err := h.Services.TOTP.Enable(r.Context(), claims.UserID, secret, code)
	if err != nil {
		http.Redirect(w, r, "/settings?profile_error=totp_invalid_code#security", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     backupCodesCookieName,
		Value:    strings.Join(rawCodes, ","),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		MaxAge:   300,
	})
	http.Redirect(w, r, "/settings#security", http.StatusSeeOther)
}

// DisableTOTP handles POST /api/user/totp/disable (form: code).
func (h *Handler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	code := r.FormValue("code")
	if code == "" {
		http.Redirect(w, r, "/settings?profile_error=totp_missing_code#security", http.StatusSeeOther)
		return
	}

	if err := h.Services.TOTP.Disable(r.Context(), claims.UserID, code); err != nil {
		http.Redirect(w, r, "/settings?profile_error=totp_invalid_code#security", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings#security", http.StatusSeeOther)
}

// PageTOTPVerify renders GET /auth/2fa — the 6-digit input page.
func (h *Handler) PageTOTPVerify(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(totpPendingCookieName); err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{BasePage: basePage(r, h.Services)}))
}

// VerifyTOTP handles POST /auth/2fa/verify (form: code, backup_code).
func (h *Handler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	pendingCookie, err := r.Cookie(totpPendingCookieName)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	userID := int64(mapClaims["sub"].(float64))

	u, err := h.Services.TOTP.StoreGetUser(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	inputCode := strings.TrimSpace(r.FormValue("code"))
	backupCode := strings.TrimSpace(r.FormValue("backup_code"))

	verified := false
	if inputCode != "" && u.TOTPSecret.Valid {
		verified = h.Services.TOTP.Verify(u.TOTPSecret.String, inputCode)
	}
	if !verified && backupCode != "" {
		if err := h.Services.TOTP.VerifyBackupCode(r.Context(), userID, backupCode); err == nil {
			verified = true
		}
	}

	if !verified {
		h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{
			BasePage: basePage(r, h.Services),
			Error:    "Invalid code. Please try again.",
		}))
		return
	}

	fullToken, err := h.Services.User.GenerateTokenForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: totpPendingCookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    fullToken,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
	h.Services.AuditLog.Record(r.Context(), r, u.ID, u.Username, model.AuditActionLogin, "user", u.ID, u.Username, nil)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
