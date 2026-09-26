package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

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
	prefs := model.NotificationPrefs{
		EmailNotifications: r.FormValue("email_notifications") == "on",
		EmailDigest:        r.FormValue("email_digest"),
		NotifyPRReview:     r.FormValue("notify_pr_review") == "on",
		NotifyMention:      r.FormValue("notify_mention") == "on",
	}
	if err := h.Services.User.UpdateNotificationPrefs(r.Context(), claims.UserID, prefs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/settings#notifications", http.StatusSeeOther)
}
