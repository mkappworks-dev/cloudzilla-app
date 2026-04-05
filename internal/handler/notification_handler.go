package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification id")
		return
	}
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.Services.Notification.MarkRead(r.Context(), id, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	notifs, _ := h.Services.Notification.List(r.Context(), claims.UserID)
	if notifs == nil {
		notifs = []model.Notification{}
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.NotificationsList(view.NotificationsFragData{Notifications: notifs}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.Services.Notification.MarkAllRead(r.Context(), claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all read")
		return
	}

	notifs, _ := h.Services.Notification.List(r.Context(), claims.UserID)
	if notifs == nil {
		notifs = []model.Notification{}
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.NotificationsList(view.NotificationsFragData{Notifications: notifs}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	count, _ := h.Services.Notification.CountUnread(r.Context(), claims.UserID)
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
