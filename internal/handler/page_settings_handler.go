package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageSettings renders the user account settings page.
func (h *Handler) PageSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	keys, err := h.Services.SSHKey.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		keys = []model.SSHKey{}
	}
	if keys == nil {
		keys = []model.SSHKey{}
	}

	h.render(w, r, pages.Settings(view.SettingsData{
		BasePage: basePage(r, h.Services),
		SSHKeys:  keys,
	}))
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
