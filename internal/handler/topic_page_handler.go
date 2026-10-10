package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageTopic renders the explore page for a single topic.
func (h *Handler) PageTopic(w http.ResponseWriter, r *http.Request) {
	topicName := chi.URLParam(r, "name")
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	sort := r.URL.Query().Get("sort")
	switch sort {
	case "updated", "name":
	default:
		sort = "stars"
	}

	repos, err := h.Services.Topic.ListReposByTopicWithStats(r.Context(), topicName, page, 20, sort)
	if err != nil {
		slog.Error("topic: list repos failed", "topic", topicName, "sort", sort, "page", page, "error", err)
		h.NotFound(w, r)
		return
	}
	if repos == nil {
		repos = []model.RepositoryWithStats{}
	}

	total, err := h.Services.Topic.CountReposByTopic(r.Context(), topicName)
	if err != nil {
		slog.Error("topic: count repos failed", "topic", topicName, "error", err)
		h.NotFound(w, r)
		return
	}

	h.render(w, r, pages.Topic(view.TopicData{
		BasePage:  basePage(r, h.Services),
		TopicName: topicName,
		Repos:     repos,
		Total:     total,
		Sort:      sort,
		Page:      page,
	}))
}
