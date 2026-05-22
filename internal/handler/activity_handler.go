package handler

import (
	"log/slog"
	"net/http"
	"net/url"
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

	filter := r.URL.Query().Get("filter")
	switch filter {
	case "yours", "watching":
	default:
		filter = "all"
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	const pageSize = 10
	events, err := h.Services.Event.Feed(r.Context(), int(claims.UserID), filter, page, pageSize+1)
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

	prevURL, nextURL := "", ""
	if page > 1 {
		prevURL = activityPageURL(filter, page-1)
	}
	if hasMore {
		nextURL = activityPageURL(filter, page+1)
	}

	scopeCounts, err := h.Services.Event.FeedCounts(r.Context(), int(claims.UserID))
	if err != nil {
		slog.Warn("activity: failed to load scope counts; showing 0", "user_id", claims.UserID, "error", err)
		scopeCounts = nil
	}

	h.render(w, r, pages.Activity(view.ActivityData{
		BasePage:    withAccountSubnav(basePage(r, h.Services), "activity", h.accountCounts(r.Context(), claims.UserID)),
		Username:    claims.Username,
		Events:      events,
		Filter:      filter,
		ScopeCounts: scopeCounts,
		Page:        page,
		HasMore:     hasMore,
		PrevURL:     prevURL,
		NextURL:     nextURL,
	}))
}

// activityPageURL builds an /activity link preserving the scope filter,
// omitting defaults (filter=all, page=1) for clean URLs.
func activityPageURL(filter string, page int) string {
	q := url.Values{}
	if filter != "" && filter != "all" {
		q.Set("filter", filter)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/activity"
	}
	return "/activity?" + q.Encode()
}
