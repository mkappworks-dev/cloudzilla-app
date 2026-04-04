package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
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

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		repoID := repo.ID
		go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventFork, map[string]any{"fork_owner": claims.Username})
	}

	dest := "/" + forked.OwnerName + "/" + forked.Name
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}
