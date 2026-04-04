package handler

import (
	"net/http"
	"strconv"

	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageFeed(w http.ResponseWriter, r *http.Request) {
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

	events, err := h.Services.Event.Feed(r.Context(), int(claims.UserID), page, 30)
	if err != nil {
		events = []model.Event{}
	}
	if events == nil {
		events = []model.Event{}
	}

	h.render(w, r, pages.Feed(view.FeedData{
		BasePage: basePage(r, h.Services),
		Events:   events,
		Page:     page,
	}))
}
