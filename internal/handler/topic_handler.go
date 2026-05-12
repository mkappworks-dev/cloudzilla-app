package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// SetTopics handles PUT /api/repos/{owner}/{repo}/topics.
// Accepts JSON body: {"topics": ["go", "web"]}.
// Responds with the repo-topics fragment when called via HTMX.
func (h *Handler) SetTopics(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var body struct {
		Topics []string `json:"topics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Topics == nil {
		body.Topics = []string{}
	}

	if err := h.Services.Topic.SetTopics(r.Context(), repo.ID, body.Topics); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	topics, _ := h.Services.Topic.ListByRepo(r.Context(), repo.ID)
	if topics == nil {
		topics = []model.Topic{}
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.RepoTopics(view.RepoTopicsFragData{
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Topics:    topics,
			CanManage: true,
		}))
		return
	}
	writeJSON(w, http.StatusOK, topics)
}

// GetTopicsFragment handles GET /api/repos/{owner}/{repo}/topics — returns the fragment.
func (h *Handler) GetTopicsFragment(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	topics, _ := h.Services.Topic.ListByRepo(r.Context(), repo.ID)
	if topics == nil {
		topics = []model.Topic{}
	}

	h.render(w, r, fragments.RepoTopics(view.RepoTopicsFragData{
		Owner:     owner,
		RepoName:  repoName,
		RepoID:    repo.ID,
		Topics:    topics,
		CanManage: canManage,
	}))
}

// PageTopic renders the explore page for a single topic.
func (h *Handler) PageTopic(w http.ResponseWriter, r *http.Request) {
	topicName := chi.URLParam(r, "name")
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	repos, _ := h.Services.Topic.ListReposByTopic(r.Context(), topicName, page, 20)
	if repos == nil {
		repos = []model.Repository{}
	}

	h.render(w, r, pages.Topic(view.TopicData{
		BasePage:  basePage(r, h.Services),
		TopicName: topicName,
		Repos:     repos,
		Page:      page,
	}))
}
