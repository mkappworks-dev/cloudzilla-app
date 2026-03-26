package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

func (h *Handler) ListDeployKeys(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	keys, err := h.Services.DeployKey.List(r.Context(), repo.ID)
	if err != nil {
		http.Error(w, "failed to list deploy keys", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) AddDeployKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	publicKey := r.FormValue("public_key")
	readOnly := r.FormValue("read_only") == "true" // checkbox sends "true" when checked; unchecked = read-write

	if title == "" || publicKey == "" {
		http.Error(w, "title and public_key are required", http.StatusBadRequest)
		return
	}

	dk, err := h.Services.DeployKey.Add(r.Context(), repo.ID, title, publicKey, readOnly)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		keys, _ := h.Services.DeployKey.List(r.Context(), repo.ID)
		h.render(w, r, fragments.DeployKeys(view.DeployKeysFragData{
			Owner:      owner,
			RepoName:   repoName,
			RepoID:     repo.ID,
			DeployKeys: keys,
			CanManage:  true,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, dk)
}

func (h *Handler) DeleteDeployKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.Services.DeployKey.Delete(r.Context(), id, repo.ID); err != nil {
		http.Error(w, "failed to delete deploy key", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		keys, _ := h.Services.DeployKey.List(r.Context(), repo.ID)
		h.render(w, r, fragments.DeployKeys(view.DeployKeysFragData{
			Owner:      owner,
			RepoName:   repoName,
			RepoID:     repo.ID,
			DeployKeys: keys,
			CanManage:  true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
