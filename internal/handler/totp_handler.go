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

// PageSecuritySettings renders GET /settings/security.
func (h *Handler) PageSecuritySettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	enabled, secret, err := h.Services.TOTP.GetUserTOTPState(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to load security settings", http.StatusInternalServerError)
		return
	}

	data := view.SecurityPageData{
		BasePage:    basePage(r, h.Services),
		TOTPEnabled: enabled,
	}

	// If TOTP is not yet enabled but a pending secret exists, show the QR setup UI.
	if !enabled && secret.Valid && secret.String != "" {
		data.TOTPSecret = secret.String
		data.OTPAuthURL = h.Services.TOTP.BuildOTPAuthURL(claims.Username, "Cloudzilla", secret.String)
	}

	h.render(w, r, pages.Security(data))
}

// PageSecuritySettingsSetup handles POST /settings/security/setup.
// It generates a new TOTP secret, stores it as pending, and re-renders the page
// showing the QR code + manual entry key.
func (h *Handler) PageSecuritySettingsSetup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	secret, otpAuthURL, err := h.Services.TOTP.Generate(claims.Username, "Cloudzilla")
	if err != nil {
		h.render(w, r, pages.Security(view.SecurityPageData{
			BasePage: basePage(r, h.Services),
			Error:    "Failed to generate secret. Please try again.",
		}))
		return
	}

	if err := h.Services.TOTP.StoreSecret(r.Context(), claims.UserID, secret); err != nil {
		h.render(w, r, pages.Security(view.SecurityPageData{
			BasePage: basePage(r, h.Services),
			Error:    "Failed to save secret. Please try again.",
		}))
		return
	}

	h.render(w, r, pages.Security(view.SecurityPageData{
		BasePage:   basePage(r, h.Services),
		TOTPSecret: secret,
		OTPAuthURL: otpAuthURL,
	}))
}

// EnableTOTP handles POST /api/user/totp/enable (form: secret, code).
func (h *Handler) EnableTOTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	secret := r.FormValue("secret")
	code := r.FormValue("code")
	if secret == "" || code == "" {
		h.render(w, r, pages.Security(view.SecurityPageData{
			BasePage:   basePage(r, h.Services),
			TOTPSecret: secret,
			OTPAuthURL: h.Services.TOTP.BuildOTPAuthURL(claims.Username, "Cloudzilla", secret),
			Error:      "Secret and code are required.",
		}))
		return
	}

	rawCodes, err := h.Services.TOTP.Enable(r.Context(), claims.UserID, secret, code)
	if err != nil {
		h.render(w, r, pages.Security(view.SecurityPageData{
			BasePage:   basePage(r, h.Services),
			TOTPSecret: secret,
			OTPAuthURL: h.Services.TOTP.BuildOTPAuthURL(claims.Username, "Cloudzilla", secret),
			Error:      "Invalid verification code. Please try again.",
		}))
		return
	}

	h.render(w, r, pages.Security(view.SecurityPageData{
		BasePage:    basePage(r, h.Services),
		TOTPEnabled: true,
		BackupCodes: rawCodes,
		Success:     "Two-factor authentication has been enabled. Save your backup codes now — they will not be shown again.",
	}))
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
		h.render(w, r, pages.Security(view.SecurityPageData{
			BasePage:    basePage(r, h.Services),
			TOTPEnabled: true,
			Error:       "Verification code is required.",
		}))
		return
	}

	if err := h.Services.TOTP.Disable(r.Context(), claims.UserID, code); err != nil {
		h.render(w, r, pages.Security(view.SecurityPageData{
			BasePage:    basePage(r, h.Services),
			TOTPEnabled: true,
			Error:       "Invalid code. Please try again.",
		}))
		return
	}

	h.render(w, r, pages.Security(view.SecurityPageData{
		BasePage: basePage(r, h.Services),
		Success:  "Two-factor authentication has been disabled.",
	}))
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
