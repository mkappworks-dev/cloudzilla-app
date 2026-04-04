package handler

import (
	"net/http"

	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageNotificationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	user, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}
	h.render(w, r, pages.NotificationSettings(view.NotificationSettingsData{
		BasePage:           basePage(r, h.Services),
		EmailNotifications: user.EmailNotifications,
		EmailDigest:        user.EmailDigest,
	}))
}

func (h *Handler) UpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	emailNotifications := r.FormValue("email_notifications") == "on"
	emailDigest := r.FormValue("email_digest")
	allowed := map[string]bool{"immediate": true, "daily": true, "weekly": true, "never": true}
	if !allowed[emailDigest] {
		emailDigest = "immediate"
	}
	if err := h.Services.User.UpdateEmailPrefs(r.Context(), claims.UserID, emailNotifications, emailDigest); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}
	http.Redirect(w, r, "/settings/notifications", http.StatusSeeOther)
}
