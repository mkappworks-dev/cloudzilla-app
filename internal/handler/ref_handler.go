package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

func (h *Handler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	name := r.FormValue("name")
	from := r.FormValue("from")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if from == "" {
		from = repo.DefaultBranch
	}
	if status, msg := h.webCommitRefusal(r.Context(), repo.ID, name); status != 0 {
		writeError(w, status, msg)
		return
	}

	if err := h.Services.Code.CreateBranch(owner, repoName, name, from); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.BranchesList(view.BranchesFragData{
			Owner:         owner,
			RepoName:      repoName,
			Branches:      result.Branches,
			CanWrite:      true,
			DefaultBranch: repo.DefaultBranch,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (h *Handler) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if name == repo.DefaultBranch {
		writeError(w, http.StatusBadRequest, "cannot delete the default branch")
		return
	}
	if err := h.Services.BranchProtection.CheckDelete(r.Context(), repo.ID, name); err != nil {
		if errors.Is(err, service.ErrForcePushBlocked) {
			writeError(w, http.StatusUnprocessableEntity, "cannot delete a branch whose protection rule blocks force pushes")
			return
		}
		if errors.Is(err, service.ErrPushRequiresPR) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		slog.Error("check branch protection", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.Services.Code.DeleteBranch(owner, repoName, name); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	result, err := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.BranchesList(view.BranchesFragData{
			Owner:         owner,
			RepoName:      repoName,
			Branches:      result.Branches,
			CanWrite:      true,
			DefaultBranch: repo.DefaultBranch,
		}))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

func (h *Handler) CreateTag(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	name := r.FormValue("name")
	from := r.FormValue("from")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if from == "" {
		from = repo.DefaultBranch
	}

	if err := h.Services.Code.CreateTag(owner, repoName, name, from); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.Services.Code.ListRefsPeeled(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.TagsList(view.TagsFragData{
			Owner:    owner,
			RepoName: repoName,
			Tags:     result.Tags,
			CanWrite: true,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (h *Handler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := h.Services.Code.DeleteTag(owner, repoName, name); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	result, err := h.Services.Code.ListRefsPeeled(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.TagsList(view.TagsFragData{
			Owner:    owner,
			RepoName: repoName,
			Tags:     result.Tags,
			CanWrite: true,
		}))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}
