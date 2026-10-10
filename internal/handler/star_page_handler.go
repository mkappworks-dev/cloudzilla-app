package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageStargazers(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}

	stargazers, _ := h.Services.Star.ListStargazers(r.Context(), owner, repoName)
	if stargazers == nil {
		stargazers = []model.User{}
	}

	starCount, _ := h.Services.Star.GetStarCount(r.Context(), repo.ID)

	h.render(w, withKnownAvatars(r, userAvatarKeys(stargazers)), pages.Stargazers(view.StargazersData{
		BasePage:   basePage(r, h.Services),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		Stargazers: stargazers,
		StarCount:  starCount,
	}))
}
