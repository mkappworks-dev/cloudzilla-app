package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const checksPageSize = 20

// PageChecks lists the repo's commits that have reported statuses, most recently updated first.
func (h *Handler) PageChecks(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	userID := viewerOf(r)
	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	commits, hasMore, err := h.Services.CommitStatus.ListRecentCommits(r.Context(), repo.ID, owner, repoName, page, checksPageSize)
	if err != nil {
		slog.Error("PageChecks: ListRecentCommits failed", "owner", owner, "repo", repoName, "page", page, "error", err)
		http.Error(w, "failed to load checks", http.StatusInternalServerError)
		return
	}

	base := "/" + owner + "/" + repoName + "/checks"
	data := view.ChecksData{
		BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "checks", canManage),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Commits:      commits,
		StatusAPIURL: h.Cfg.Server.BaseURL + "/api/repos/" + owner + "/" + repoName + "/statuses/<sha>",
	}
	if page > 1 {
		data.NewerURL = base + "?page=" + strconv.Itoa(page-1)
	}
	if hasMore {
		data.OlderURL = base + "?page=" + strconv.Itoa(page+1)
	}
	h.render(w, r, pages.Checks(data))
}
