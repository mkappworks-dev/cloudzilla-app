package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const (
	backupCodesCookieName = "cz_backup_codes"
	newTokenCookieName    = "cz_new_token"
)

// setSettingsFlash hands a one-time secret to the next /settings render in a
// cookie, keeping it out of URLs, browser history, proxy logs and Referer.
func (h *Handler) setSettingsFlash(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/settings", MaxAge: 300,
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

// takeSettingsFlash returns a value set by setSettingsFlash and expires it, so it renders once.
func (h *Handler) takeSettingsFlash(w http.ResponseWriter, r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil || c.Value == "" {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/settings", MaxAge: -1,
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	return c.Value
}

func (h *Handler) PageSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	user, err := h.Services.User.GetByID(ctx, claims.UserID)
	if err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}

	keys, _ := h.Services.SSHKey.ListByUser(ctx, claims.UserID)
	if keys == nil {
		keys = []model.SSHKey{}
	}
	tokens, _ := h.Services.AccessToken.List(ctx, claims.UserID)
	replies, _ := h.Services.SavedReply.List(ctx, claims.UserID)
	apps, _ := h.Services.OAuthApp.ListByOwner(ctx, claims.UserID)
	auths, _ := h.Services.OAuthApp.ListAuthorizationsByUser(ctx, claims.UserID)

	enabled, secret, _ := h.Services.TOTP.GetUserTOTPState(ctx, claims.UserID)
	data := view.SettingsData{
		BasePage:            withAccountSubnav(basePage(r, h.Services), "settings", h.accountCounts(ctx, claims.UserID)),
		User:                *user,
		SSHKeys:             keys,
		Tokens:              tokens,
		SavedReplies:        replies,
		OAuthApps:           apps,
		OAuthAuthorizations: auths,
		TOTPEnabled:         enabled,
		NewToken:            h.takeSettingsFlash(w, r, newTokenCookieName),
		NoreplyEmail:        h.Services.User.NoreplyEmail(ctx, user),
		GoogleConfigured:    h.Cfg.OAuth.GoogleClientID != "",
		GoogleConnected:     user.OAuthProvider == googleProvider,
		HasPassword:         user.PasswordHash != "",
		Confirm:             h.confirmFactors(r, claims.UserID),

		ConnectedAccountsNotice: h.takeSettingsFlash(w, r, settingsNoticeCookieName),
	}
	data.EmailVerificationAvailable = h.Services.EmailVerifier.Available()
	if data.EmailVerificationAvailable && !user.EmailVerified() {
		if data.VerificationLinkSent, err = h.Services.EmailVerifier.LinkPending(ctx, claims.UserID); err != nil {
			slog.Error("check pending verification link", "user_id", claims.UserID, "error", err)
		}
	}
	if !enabled && secret.Valid && secret.String != "" {
		data.TOTPPendingSecret = secret.String
		if data.TOTPQRCode, err = h.Services.TOTP.EnrolmentQRCode(ctx, claims.Username, "Cloudzilla", secret.String); err != nil {
			slog.Error("render totp qr code", "user_id", claims.UserID, "error", err)
		}
	}

	if codes := h.takeSettingsFlash(w, r, backupCodesCookieName); codes != "" {
		data.BackupCodes = strings.Split(codes, ",")
	}
	if data.NewToken != "" || len(data.BackupCodes) > 0 {
		w.Header().Set("Cache-Control", "no-store")
	}

	data.SessionsRevoked = r.URL.Query().Get("sessions_revoked") == "1"
	data.PasswordChanged = r.URL.Query().Get("password_changed") == "1"
	data.PasswordError = r.URL.Query().Get("password_error")
	if r.URL.Query().Get("profile_saved") == "1" {
		data.ProfileSaved = true
	}
	if e := r.URL.Query().Get("profile_error"); strings.HasPrefix(e, "google_") {
		data.ConnectedAccountsError = e
	} else if e != "" {
		data.ProfileError = e
	}

	h.render(w, r, pages.Settings(data))
}

// UpdateProfile handles POST /settings/profile.
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		settingsError(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := r.ParseForm(); err != nil {
		settingsError(w, r, http.StatusBadRequest, "bad request")
		return
	}
	refuse := func(code string) { refuseSettingsForm(w, r, profileFormError, "profile", code) }
	before, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		refuse("update_failed")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	err = h.Services.User.UpdateProfile(
		r.Context(),
		claims.UserID,
		r.FormValue("name"),
		email,
		r.FormValue("bio"),
		r.FormValue("company"),
		r.FormValue("location"),
		confirmationFrom(r),
	)
	if _, code, refused := reauthRefusal(claims.UserID, err); refused {
		refuse(code)
		return
	}
	if err != nil {
		switch err {
		case service.ErrInvalidEmail:
			refuse("invalid_email")
		case service.ErrEmailTaken:
			refuse("email_taken")
		default:
			refuse("update_failed")
		}
		return
	}
	if before.Email != email {
		h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionEmailChange, model.AuditTargetUser, claims.UserID, claims.Username,
			map[string]any{"from": before.Email, "to": email})
	}
	redirectAfterSave(w, r, "/settings?profile_saved=1#profile")
}

const profileFormError = "#profile-form-error"

// ChangePassword handles POST /settings/password. The change ends every
// session, so this browser gets a new one.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	refuse := func(code string) {
		http.Redirect(w, r, "/settings?password_error="+code+"#password", http.StatusSeeOther)
	}
	newPassword := r.FormValue("new_password")
	if newPassword != r.FormValue("new_password_confirm") {
		refuse("password_mismatch")
		return
	}
	token, err := h.Services.User.ChangePassword(r.Context(), claims.UserID, confirmationFrom(r), newPassword)
	if err != nil {
		code := "password_change_failed"
		switch {
		case errors.Is(err, service.ErrPasswordTooShort):
			code = "password_too_short"
		case errors.Is(err, service.ErrPasswordTooLong):
			code = "password_too_long"
		default:
			if _, c, refused := reauthRefusal(claims.UserID, err); refused {
				code = c
			} else {
				slog.Error("change password", "user_id", claims.UserID, "error", err)
			}
		}
		refuse(code)
		return
	}
	h.setAuthCookie(w, token)
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionPasswordChange, model.AuditTargetUser, claims.UserID, claims.Username, nil)
	http.Redirect(w, r, "/settings?password_changed=1#password", http.StatusSeeOther)
}

// DeleteAccount handles POST /settings/delete-account.
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(r.FormValue("confirm_username")) != claims.Username {
		http.Redirect(w, r, "/settings?profile_error=delete_confirm_mismatch#delete", http.StatusSeeOther)
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		redirectReauthRefusal(w, r, claims.UserID, err, "", "delete")
		return
	}
	if err := h.Services.User.DeleteUser(r.Context(), claims.UserID); err != nil {
		code := "delete_failed"
		switch {
		case errors.Is(err, service.ErrSoleOrgOwner):
			code = "sole_org_owner"
		case errors.Is(err, service.ErrLastSuperadmin):
			code = "last_superadmin"
		default:
			slog.Error("delete account failed", "user_id", claims.UserID, "error", err)
		}
		http.Redirect(w, r, "/settings?profile_error="+code+"#delete", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: h.Cfg.Auth.CookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// PageNotifications renders the in-app notifications page.
func (h *Handler) PageNotifications(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	notifs, _ := h.Services.Notification.List(r.Context(), claims.UserID)
	if notifs == nil {
		notifs = []model.Notification{}
	}
	unread, _ := h.Services.Notification.CountUnread(r.Context(), claims.UserID)

	h.render(w, r, pages.Notifications(view.NotificationsData{
		BasePage:      basePage(r, h.Services),
		Notifications: notifs,
		UnreadCount:   unread,
	}))
}
