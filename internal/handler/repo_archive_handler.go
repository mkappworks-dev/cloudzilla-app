package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (h *Handler) ArchiveRepo(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Services.Repo.Archive(r.Context(), repo.ID, claims.UserID); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeError(w, http.StatusForbidden, "only the repo owner can archive this repository")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to archive repository")
		}
		return
	}
	w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/settings")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnarchiveRepo(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Services.Repo.Unarchive(r.Context(), repo.ID, claims.UserID); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeError(w, http.StatusForbidden, "only the repo owner can unarchive this repository")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to unarchive repository")
		}
		return
	}
	w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/settings")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteRepo(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Services.Repo.Delete(r.Context(), repo.ID, claims.UserID); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeError(w, http.StatusForbidden, "only the repo owner can delete this repository")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to delete repository")
		}
		return
	}
	http.Redirect(w, r, "/"+owner, http.StatusSeeOther)
}

func (h *Handler) SetRepoTemplate(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	isTemplate := r.FormValue("is_template") == "true" || r.FormValue("is_template") == "on"
	if err := h.Services.Repo.SetTemplate(r.Context(), repo.ID, claims.UserID, isTemplate); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeError(w, http.StatusForbidden, "only the repo owner can change template status")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update template status")
		}
		return
	}
	w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/settings")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var templateRepoID int64
	if v := r.FormValue("template_repo_id"); v != "" {
		if id, err := parseInt64(v); err == nil {
			templateRepoID = id
		}
	}
	newName := r.FormValue("name")
	description := r.FormValue("description")
	if templateRepoID == 0 || newName == "" {
		writeError(w, http.StatusBadRequest, "template_repo_id and name are required")
		return
	}
	repo, err := h.Services.Repo.CreateFromTemplate(r.Context(), templateRepoID, claims.UserID, claims.Username, newName, description)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	http.Redirect(w, r, "/"+claims.Username+"/"+repo.Name, http.StatusSeeOther)
}

func parseInt64(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
