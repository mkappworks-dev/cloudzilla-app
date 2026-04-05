package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageDependencies(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "repo not found", http.StatusNotFound)
		} else {
			slog.Error("dependency: failed to get repo", "owner", owner, "repo", repoName, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	deps, err := h.Services.Dependency.ListByRepo(r.Context(), repo.ID)
	if err != nil {
		slog.Error("dependency: failed to list dependencies", "repo_id", repo.ID, "error", err)
		http.Error(w, "failed to load dependencies", http.StatusInternalServerError)
		return
	}
	if deps == nil {
		deps = []model.RepoDependency{}
	}

	byManager := make(map[string][]model.RepoDependency)
	for _, d := range deps {
		byManager[d.PackageMgr] = append(byManager[d.PackageMgr], d)
	}

	h.render(w, r, pages.Dependencies(view.DependenciesData{
		BasePage:     basePage(r, h.Services),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Dependencies: deps,
		ByManager:    byManager,
	}))
}
