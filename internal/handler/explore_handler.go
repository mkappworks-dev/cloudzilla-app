package handler

import (
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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
		http.Error(w, "Failed to load explore page", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []model.RepositoryWithStats{}
	}
	data.Repos = repos

	h.render(w, r, pages.Explore(data))
}
