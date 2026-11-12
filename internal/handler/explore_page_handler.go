package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageExplore(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	switch tab {
	case "newest", "forked", "trending":
		// valid
	default:
		tab = "trending"
	}

	period := r.URL.Query().Get("period")
	switch period {
	case "daily", "weekly", "monthly":
		// valid
	default:
		period = "weekly"
	}

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	data := view.ExploreData{
		BasePage: basePage(r, h.Services),
		Tab:      tab,
		Period:   period,
		Query:    strings.TrimSpace(r.URL.Query().Get("q")),
		Language: r.URL.Query().Get("language"),
		Page:     page,
		PageSize: service.ExplorePageSize,
	}

	repos, total, err := h.Services.Explore.Repos(r.Context(), service.ExploreQuery{
		Tab: tab, Period: period, Text: data.Query, Language: data.Language, Page: page,
	})
	if err != nil {
		slog.Error("explore: failed to load repositories", "tab", tab, "period", period, "error", err)
		h.ServerError(w, r)
		return
	}
	if repos == nil {
		repos = []model.RepositoryWithStats{}
	}
	data.Repos, data.Total = repos, total

	if langs, err := h.Services.Explore.Languages(r.Context()); err != nil {
		slog.Warn("explore: language filter unavailable", "error", err)
	} else {
		data.Languages = langs
	}

	h.render(w, r, pages.Explore(data))
}
