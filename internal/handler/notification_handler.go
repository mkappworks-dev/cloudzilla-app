package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// notificationsData builds the inbox view for the ?filter and ?page in r.
// The mark-read endpoints reuse it so the re-rendered view stays on the caller's page.
func (h *Handler) notificationsData(r *http.Request, userID int64) (view.NotificationsData, error) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	np, err := h.Services.Notification.ListPage(r.Context(), userID, r.URL.Query().Get("filter"), page)
	if err != nil {
		return view.NotificationsData{}, err
	}
	return view.NotificationsData{
		BasePage:      basePage(r, h.Services),
		Notifications: np.Items,
		Filter:        np.Filter,
		Page:          np.Page,
		TotalPages:    np.TotalPages,
		PerPage:       service.NotificationsPerPage,
		Total:         np.Total,
		InboxCount:    np.InboxCount,
		UnreadCount:   np.UnreadCount,
		ReadCount:     np.ReadCount,
	}, nil
}

// renderNotificationsInbox renders the HTMX swap target, or a 500 when the list can't load.
func (h *Handler) renderNotificationsInbox(w http.ResponseWriter, r *http.Request, userID int64) {
	data, err := h.notificationsData(r, userID)
	if err != nil {
		slog.Error("notifications: list failed", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load notifications")
		return
	}
	h.render(w, r, pages.NotificationsInbox(data))
}

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

	if r.Header.Get("HX-Request") == "true" {
		h.renderNotificationsInbox(w, r, claims.UserID)
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

	if r.Header.Get("HX-Request") == "true" {
		h.renderNotificationsInbox(w, r, claims.UserID)
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
