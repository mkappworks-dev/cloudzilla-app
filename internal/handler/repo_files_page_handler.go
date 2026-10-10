package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageNewFile renders the create-file / upload page.
func (h *Handler) PageNewFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ref, dir := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	dir = strings.Trim(dir, "/")
	if ref == "" {
		ref = repo.DefaultBranch
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.NewFile(view.NewFileData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
		Owner:    owner,
		RepoName: repoName,
		Ref:      ref,
		Dir:      dir,
	}))
}

// PageEditFile renders the editor for a text file on a branch.
func (h *Handler) PageEditFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, ok := h.editableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	ref, path := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	file, ok := h.branchFile(w, r, owner, repoName, ref, path)
	if !ok {
		return
	}
	if file == nil {
		h.NotFound(w, r)
		return
	}
	if reason := editRefusal(file); reason != "" {
		http.Error(w, reason, http.StatusUnprocessableEntity)
		return
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.EditFile(view.EditFileData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
		Owner:    owner,
		RepoName: repoName,
		Ref:      ref,
		Path:     path,
		BlobSHA:  file.SHA,
		NewPath:  path,
		Content:  string(file.Content),
	}))
}
