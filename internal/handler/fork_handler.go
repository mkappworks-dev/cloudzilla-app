package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
)

func (h *Handler) ForkRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	forked, err := h.Services.Repo.Fork(r.Context(), owner, repoName, claims.UserID, claims.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	dest := "/" + forked.OwnerName + "/" + forked.Name
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}
