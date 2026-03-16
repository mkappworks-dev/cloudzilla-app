package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
)

func (h *Handler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
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
		h.renderFragment(w, "fragment-branches-list", BranchesFragData{
			Owner:         owner,
			RepoName:      repoName,
			Branches:      result.Branches,
			CanWrite:      true,
			DefaultBranch: repo.DefaultBranch,
		})
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
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

	if err := h.Services.Code.DeleteBranch(owner, repoName, name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-branches-list", BranchesFragData{
			Owner:         owner,
			RepoName:      repoName,
			Branches:      result.Branches,
			CanWrite:      true,
			DefaultBranch: repo.DefaultBranch,
		})
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
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

	result, err := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-tags-list", TagsFragData{
			Owner:    owner,
			RepoName: repoName,
			Tags:     result.Tags,
			CanWrite: true,
		})
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := h.Services.Code.DeleteTag(owner, repoName, name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list refs")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-tags-list", TagsFragData{
			Owner:    owner,
			RepoName: repoName,
			Tags:     result.Tags,
			CanWrite: true,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}
