package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageActions(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	userID := viewerOf(r)

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Actions(view.ActionsData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "actions", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
	}))
}
