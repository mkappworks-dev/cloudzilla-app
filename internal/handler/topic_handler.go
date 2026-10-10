package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
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

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
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

	topics, lerr := h.Services.Topic.ListByRepo(r.Context(), repo.ID)
	if lerr != nil {
		slog.Warn("set topics: reload failed; pill list may render stale", "repo_id", repo.ID, "error", lerr)
	}
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

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	topics, lerr := h.Services.Topic.ListByRepo(r.Context(), repo.ID)
	if lerr != nil {
		slog.Warn("topics fragment: list failed", "repo_id", repo.ID, "error", lerr)
	}
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
