package handler

import (
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const backupCodesCookieName = "cz_backup_codes"

// PageSettings renders the consolidated account settings page.
// It loads profile, SSH keys, access tokens, notification prefs, TOTP state,
// and any one-shot data (backup codes after enabling 2FA, new-token reveal).
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

	enabled, secret, _ := h.Services.TOTP.GetUserTOTPState(ctx, claims.UserID)
	data := view.SettingsData{
		BasePage:    withAccountSubnav(basePage(r, h.Services), "settings", h.accountCounts(ctx, claims.UserID)),
		User:        *user,
		SSHKeys:     keys,
		Tokens:      tokens,
		TOTPEnabled: enabled,
		NewToken:    r.URL.Query().Get("new_token"),
	}
	if !enabled && secret.Valid && secret.String != "" {
		data.TOTPPendingSecret = secret.String
		data.TOTPOTPAuthURL = h.Services.TOTP.BuildOTPAuthURL(claims.Username, "Cloudzilla", secret.String)
	}

	if c, err := r.Cookie(backupCodesCookieName); err == nil && c.Value != "" {
		data.BackupCodes = strings.Split(c.Value, ",")
		http.SetCookie(w, &http.Cookie{
			Name: backupCodesCookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure,
		})
	}

	if r.URL.Query().Get("profile_saved") == "1" {
		data.ProfileSaved = true
	}
	if e := r.URL.Query().Get("profile_error"); e != "" {
		data.ProfileError = e
	}

	h.render(w, r, pages.Settings(data))
}

// UpdateProfile handles POST /settings/profile.
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	err := h.Services.User.UpdateProfile(
		r.Context(),
		claims.UserID,
		r.FormValue("name"),
		strings.TrimSpace(r.FormValue("username")),
		strings.TrimSpace(r.FormValue("email")),
		r.FormValue("bio"),
		r.FormValue("company"),
		r.FormValue("location"),
	)
	if err != nil {
		switch err {
		case service.ErrInvalidUsername:
			http.Redirect(w, r, "/settings?profile_error=invalid_username#profile", http.StatusSeeOther)
		case service.ErrUsernameTaken:
			http.Redirect(w, r, "/settings?profile_error=username_taken#profile", http.StatusSeeOther)
		case service.ErrInvalidEmail:
			http.Redirect(w, r, "/settings?profile_error=invalid_email#profile", http.StatusSeeOther)
		case service.ErrEmailTaken:
			http.Redirect(w, r, "/settings?profile_error=email_taken#profile", http.StatusSeeOther)
		default:
			http.Redirect(w, r, "/settings?profile_error=update_failed#profile", http.StatusSeeOther)
		}
		return
	}
	http.Redirect(w, r, "/settings?profile_saved=1#profile", http.StatusSeeOther)
}

// DeleteAccount handles POST /settings/delete-account. Requires the form field
// `confirm_username` to match the user's current username, as a paranoia guard.
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
	if err := h.Services.User.DeleteUser(r.Context(), claims.UserID); err != nil {
		http.Redirect(w, r, "/settings?profile_error=delete_failed#delete", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: h.Cfg.Auth.CookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// RequestExport handles POST /settings/export. For now this is a no-op stub
// that just acknowledges the request — actual archive generation is future work.
func (h *Handler) RequestExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/settings?profile_saved=1#delete", http.StatusSeeOther)
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
