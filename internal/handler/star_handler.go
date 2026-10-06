package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) StarRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	if err := h.Services.Star.Star(r.Context(), owner, repoName, claims.UserID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	repoID := repo.ID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventStar, map[string]any{})

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
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}

	if err := h.Services.Star.Unstar(r.Context(), owner, repoName, claims.UserID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	stargazers, _ := h.Services.Star.ListStargazers(r.Context(), owner, repoName)
	if stargazers == nil {
		stargazers = []model.User{}
	}

	starCount, _ := h.Services.Star.GetStarCount(r.Context(), repo.ID)

	if r.Header.Get("HX-Request") == "true" {
		out := make([]publicUser, len(stargazers))
		for i, u := range stargazers {
			out[i] = newPublicUser(u, h.Cfg.Server.BaseURL)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	h.render(w, withKnownAvatars(r, userAvatarKeys(stargazers)), pages.Stargazers(view.StargazersData{
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
