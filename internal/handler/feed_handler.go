package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageActivity(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	const pageSize = 30
	events, err := h.Services.Event.Feed(r.Context(), int(claims.UserID), page, pageSize+1)
	if err != nil {
		slog.Error("activity: failed to load events", "user_id", claims.UserID, "error", err)
		events = []model.Event{}
	}
	if events == nil {
		events = []model.Event{}
	}
	hasMore := len(events) > pageSize
	if hasMore {
		events = events[:pageSize]
	}

	h.render(w, r, pages.Activity(view.ActivityData{
		BasePage: basePage(r, h.Services),
		Username: claims.Username,
		Events:   events,
		Page:     page,
		HasMore:  hasMore,
	}))
}

func (h *Handler) PageFeed(w http.ResponseWriter, r *http.Request) {
	h.PageActivity(w, r)
}
