package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
)

func (h *Handler) RestoreRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, err := h.Services.Repo.GetDeleted(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	if err := h.Services.Repo.Restore(r.Context(), repo.ID, claims.UserID, claims.IsSuperadmin); err != nil {
		// A deleted repo is visible only to those who may restore it.
		if strings.HasPrefix(err.Error(), "forbidden") {
			writeError(w, http.StatusNotFound, "repo not found")
		} else {
			writeError(w, http.StatusInternalServerError, "restore failed")
		}
		return
	}

	http.Redirect(w, r, "/"+owner+"/"+repoName, http.StatusSeeOther)
}
