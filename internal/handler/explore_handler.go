package handler

import (
	"log/slog"
	"net/http"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageExplore(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "trending"
	}

	period := r.URL.Query().Get("period")
	switch period {
	case "daily", "weekly", "monthly":
		// valid
	default:
		period = "weekly"
	}

	data := view.ExploreData{
		BasePage: basePage(r, h.Services),
		Tab:      tab,
		Period:   period,
	}

	var (
		repos []model.RepositoryWithStats
		err   error
	)
	switch tab {
	case "newest":
		repos, err = h.Services.Explore.Newest(r.Context())
	case "forked":
		repos, err = h.Services.Explore.MostForked(r.Context())
	default:
		repos, err = h.Services.Explore.Trending(r.Context(), period)
	}
	if err != nil {
		slog.Error("explore: failed to load repositories", "tab", tab, "period", period, "error", err)
	}
	if repos == nil {
		repos = []model.RepositoryWithStats{}
	}
	data.Repos = repos

	h.render(w, r, pages.Explore(data))
}
