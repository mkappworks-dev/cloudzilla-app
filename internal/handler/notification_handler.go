package handler

import (
	"context"
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
// The returned request carries the actors' avatars and is the one to render with.
func (h *Handler) notificationsData(r *http.Request, userID int64) (view.NotificationsData, *http.Request, error) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	np, err := h.Services.Notification.ListPage(r.Context(), userID, r.URL.Query().Get("filter"), page)
	if err != nil {
		return view.NotificationsData{}, r, err
	}
	actors := make([]string, len(np.Items))
	for i, n := range np.Items {
		actors[i] = n.ActorName
	}
	r = h.withAvatars(r, actors...)
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
		WatchedRepos:  np.WatchedRepos,
	}, r, nil
}

// maxBulkNotifications bounds one bulk request; a page holds 25 rows.
const maxBulkNotifications = 100

// bulkNotificationIDs reads the repeated "ids" form values, dropping anything that isn't a positive integer.
func bulkNotificationIDs(r *http.Request) []int64 {
	if err := r.ParseForm(); err != nil {
		return nil
	}
	var ids []int64
	for _, v := range r.Form["ids"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
		if len(ids) == maxBulkNotifications {
			break
		}
	}
	return ids
}

// MarkNotificationsDone marks the selected notifications read.
func (h *Handler) MarkNotificationsDone(w http.ResponseWriter, r *http.Request) {
	h.bulkNotificationAction(w, r, "failed to mark done", h.Services.Notification.MarkReadMany)
}

// UnsubscribeNotifications stops watching the repos behind the selected notifications.
func (h *Handler) UnsubscribeNotifications(w http.ResponseWriter, r *http.Request) {
	h.bulkNotificationAction(w, r, "failed to unsubscribe", h.Services.Notification.UnsubscribeFromRepos)
}

func (h *Handler) bulkNotificationAction(w http.ResponseWriter, r *http.Request, failMsg string, act func(ctx context.Context, userID int64, ids []int64) error) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := act(r.Context(), claims.UserID, bulkNotificationIDs(r)); err != nil {
		slog.Error("notifications: bulk action failed", "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, failMsg)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderNotificationsInbox(w, r, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// renderNotificationsInbox renders the HTMX swap target, or a 500 when the list can't load.
func (h *Handler) renderNotificationsInbox(w http.ResponseWriter, r *http.Request, userID int64) {
	data, r, err := h.notificationsData(r, userID)
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
