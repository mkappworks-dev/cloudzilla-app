package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageForkRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		h.Unauthorized(w, r)
		return
	}
	ctx := r.Context()
	repo, ok := h.readableRepo(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return
	}

	var owners []string
	if !strings.EqualFold(claims.Username, repo.OwnerName) {
		owners = append(owners, claims.Username)
	}
	orgs, err := h.Services.Org.ListOwnedByUser(ctx, claims.UserID)
	if err != nil {
		slog.Error("fork page: list owned orgs", "user_id", claims.UserID, "error", err)
	}
	for _, o := range orgs {
		if !strings.EqualFold(o.Name, repo.OwnerName) {
			owners = append(owners, o.Name)
		}
	}

	forks, err := h.Services.Repo.ForksOwnedBy(ctx, repo.ID, claims.UserID)
	if err != nil {
		slog.Error("fork page: list existing forks", "repo_id", repo.ID, "error", err)
	}
	existing := make([]view.RepoRef, 0, len(forks))
	for _, f := range forks {
		existing = append(existing, view.RepoRef{Name: f.OwnerName + "/" + f.Name, Path: "/" + f.OwnerName + "/" + f.Name})
	}

	var defaultBranch string
	if _, _, err := h.Services.Code.ResolveRef(repo.OwnerName, repo.Name, repo.DefaultBranch); err == nil {
		defaultBranch = repo.DefaultBranch
	}

	h.render(w, r, pages.RepoFork(view.RepoForkData{
		BasePage:      basePage(r, h.Services),
		SourceOwner:   repo.OwnerName,
		SourceName:    repo.Name,
		Description:   repo.Description,
		Private:       repo.Private,
		DefaultBranch: defaultBranch,
		Owners:        owners,
		ExistingForks: existing,
	}))
}
