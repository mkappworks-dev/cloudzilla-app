package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// In-process cache of last lazy-parse times per repo to keep an empty
// dependency set from triggering a full manifest tree-walk on every page view.
var (
	depLazyParseTTL  = time.Hour
	depLazyParsedMu  sync.Mutex
	depLazyParsedAt  = map[int64]time.Time{}
)

func shouldLazyParseDeps(repoID int64) bool {
	depLazyParsedMu.Lock()
	defer depLazyParsedMu.Unlock()
	if last, ok := depLazyParsedAt[repoID]; ok && time.Since(last) < depLazyParseTTL {
		return false
	}
	depLazyParsedAt[repoID] = time.Now()
	return true
}

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
	if len(deps) == 0 && shouldLazyParseDeps(repo.ID) {
		if parseErr := h.Services.Dependency.ParseAndStore(r.Context(), repo); parseErr != nil {
			slog.Warn("dependency: lazy parse failed", "repo_id", repo.ID, "error", parseErr)
		} else {
			refreshed, listErr := h.Services.Dependency.ListByRepo(r.Context(), repo.ID)
			if listErr != nil {
				slog.Warn("dependency: lazy re-list failed", "repo_id", repo.ID, "error", listErr)
			} else {
				deps = refreshed
			}
		}
	}

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Dependencies(view.DependenciesData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Groups:   groupDependencies(deps),
	}))
}

func groupDependencies(deps []model.RepoDependency) []components.DependencyGroupData {
	byMgr := map[string][]components.DependencyRow{}
	for _, d := range deps {
		byMgr[d.PackageMgr] = append(byMgr[d.PackageMgr], components.DependencyRow{
			Package: d.Package, Version: d.Version, IsDev: d.IsDev,
		})
	}
	out := make([]components.DependencyGroupData, 0, len(byMgr))
	for mgr, rows := range byMgr {
		out = append(out, components.DependencyGroupData{
			PackageMgr: mgr,
			Label:      manifestLabel(mgr),
			Rows:       rows,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PackageMgr < out[j].PackageMgr })
	return out
}

func manifestLabel(mgr string) string {
	switch mgr {
	case "go":
		return "go.mod"
	case "npm":
		return "package.json"
	case "pip":
		return "Python"
	case "cargo":
		return "Cargo.toml"
	default:
		return mgr
	}
}
