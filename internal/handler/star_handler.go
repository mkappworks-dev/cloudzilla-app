package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) StarRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	if err := h.Services.Star.Star(r.Context(), owner, repoName, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderStarButtonFragment(w, r, owner, repoName, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnstarRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	if err := h.Services.Star.Unstar(r.Context(), owner, repoName, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderStarButtonFragment(w, r, owner, repoName, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListStargazers(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	stargazers, _ := h.Services.Star.ListStargazers(r.Context(), owner, repoName)
	if stargazers == nil {
		stargazers = []model.User{}
	}

	starCount, _ := h.Services.Star.GetStarCount(r.Context(), repo.ID)

	if r.Header.Get("HX-Request") == "true" {
		writeJSON(w, http.StatusOK, stargazers)
		return
	}

	h.render(w, r, pages.Stargazers(view.StargazersData{
		BasePage:   basePage(r, h.Services),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		Stargazers: stargazers,
		StarCount:  starCount,
	}))
}

func (h *Handler) PageStargazers(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	stargazers, _ := h.Services.Star.ListStargazers(r.Context(), owner, repoName)
	if stargazers == nil {
		stargazers = []model.User{}
	}

	starCount, _ := h.Services.Star.GetStarCount(r.Context(), repo.ID)

	h.render(w, r, pages.Stargazers(view.StargazersData{
		BasePage:   basePage(r, h.Services),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		Stargazers: stargazers,
		StarCount:  starCount,
	}))
}

func (h *Handler) PageUserStars(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "owner")

	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	repos, _ := h.Services.Star.ListByUser(r.Context(), username)
	if repos == nil {
		repos = []model.Repository{}
	}

	h.render(w, r, pages.UserStars(view.UserStarsData{
		BasePage:    basePage(r, h.Services),
		ProfileUser: *user,
		Repos:       repos,
	}))
}

func (h *Handler) renderStarButtonFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, userID int64) {
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	count, _ := h.Services.Star.GetStarCount(r.Context(), repo.ID)
	isStarred, _ := h.Services.Star.IsStarred(r.Context(), repo.ID, userID)
	h.render(w, r, fragments.StarButton(view.StarButtonData{
		Owner:     owner,
		RepoName:  repoName,
		RepoID:    repo.ID,
		Count:     count,
		IsStarred: isStarred,
		LoggedIn:  true,
	}))
}
