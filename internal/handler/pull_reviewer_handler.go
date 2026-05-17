package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// AddPullReviewer requests a review from a repo collaborator named in the form.
func (h *Handler) AddPullReviewer(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	username := r.FormValue("username")
	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	users, err := h.Services.User.GetManyByUsernames(r.Context(), []string{username})
	if err != nil || len(users) == 0 {
		writeError(w, http.StatusBadRequest, "unknown user")
		return
	}
	user := users[0]
	// Only request reviews from users who can read the repo, so a crafted
	// POST cannot pull arbitrary accounts into the PR.
	if !h.Services.Repo.CanRead(r.Context(), authRepo, &user.ID) {
		writeError(w, http.StatusForbidden, "user cannot access repo")
		return
	}

	if err := h.Services.PullReview.RequestReviewers(r.Context(), owner, repoName, number, []model.User{user}); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Review requested")
	}
	w.WriteHeader(http.StatusNoContent)
}
