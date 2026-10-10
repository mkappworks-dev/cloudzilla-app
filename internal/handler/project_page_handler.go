package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageProjects(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowProjects {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	list, err := h.Services.Project.ListByRepoWithStats(r.Context(), owner, repoName, state, query)
	if err != nil {
		http.Error(w, "failed to load projects", http.StatusInternalServerError)
		return
	}

	h.render(w, r, pages.Projects(view.ProjectsData{
		BasePage:    h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "projects", canManage),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Items:       list.Projects,
		StateFilter: state,
		SearchQuery: query,
		OpenCount:   list.OpenCount,
		ClosedCount: list.ClosedCount,
		CanWrite:    canWrite,
		CanManage:   canManage,
	}))
}

func (h *Handler) PageProjectDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowProjects {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	canWrite := false
	canManage := false
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		viewerID = &claims.UserID
	}

	project, err := h.Services.Project.GetProject(r.Context(), projectID)
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if project.RepoID != repo.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	columns, err := h.Services.Project.ListColumnsWithCardsExpanded(r.Context(), project.ID, viewerID)
	if err != nil {
		http.Error(w, "failed to load board", http.StatusInternalServerError)
		return
	}
	if columns == nil {
		columns = []service.KanbanColumnView{}
	}

	data := view.ProjectDetailData{
		BasePage:  h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "projects", canManage),
		Repo:      *repo,
		Owner:     owner,
		RepoName:  repoName,
		Project:   *project,
		Columns:   columns,
		CanWrite:  canWrite,
		CanManage: canManage,
	}
	if canWrite {
		h.loadCardOptions(r, &data, repo)
	}
	var names []string
	for _, p := range data.People {
		names = append(names, p.Username)
	}
	for _, col := range columns {
		for _, c := range col.Cards {
			for _, a := range c.Assignees {
				names = append(names, a.Username)
			}
		}
	}
	h.render(w, h.withAvatars(r, names...), pages.ProjectDetail(data))
}

// loadCardOptions fills the card modal's label and people lists. Best-effort: a failed
// query leaves that picker empty rather than failing the board.
func (h *Handler) loadCardOptions(r *http.Request, data *view.ProjectDetailData, repo *model.Repository) {
	ctx := r.Context()
	if labels, err := h.Services.Label.ListByRepo(ctx, data.Owner, data.RepoName); err == nil {
		data.Labels = labels
	} else {
		slog.Warn("project board: list labels failed", "repo_id", repo.ID, "error", err)
	}
	seen := map[int64]bool{}
	if repo.OwnerID != 0 {
		data.People = append(data.People, model.CardUser{ID: repo.OwnerID, Username: repo.OwnerName})
		seen[repo.OwnerID] = true
	}
	collabs, err := h.Services.Repo.ListCollaborators(ctx, repo.ID)
	if err != nil {
		slog.Warn("project board: list collaborators failed", "repo_id", repo.ID, "error", err)
	}
	for _, c := range collabs {
		if !seen[c.UserID] && c.Username != "" {
			seen[c.UserID] = true
			data.People = append(data.People, model.CardUser{ID: c.UserID, Username: c.Username})
		}
	}
}
