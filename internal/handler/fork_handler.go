package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type forkRepoRequest struct {
	Owner             string  `json:"owner"`
	Name              string  `json:"name"`
	Description       *string `json:"description"`
	DefaultBranchOnly bool    `json:"default_branch_only"`
}

func (h *Handler) ForkRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	// A non-JSON body means "fork with the defaults", so form and htmx callers keep working.
	jsonBody := isJSON(r)
	var req forkRepoRequest
	if jsonBody {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if !tokenMayForkInto(claims, req.Owner) {
		writeError(w, http.StatusForbidden, "this token isn't allowed for that repository or organization")
		return
	}

	forked, err := h.Services.Repo.Fork(r.Context(), owner, repoName, claims.UserID, claims.Username, service.ForkOptions{
		Owner:             req.Owner,
		Name:              req.Name,
		Description:       req.Description,
		DefaultBranchOnly: req.DefaultBranchOnly,
	})
	switch {
	case errors.Is(err, service.ErrForbidden):
		writeError(w, http.StatusForbidden, "you can fork only into your account or an organization you own")
		return
	case errors.Is(err, service.ErrForkIntoSourceOwner), errors.Is(err, service.ErrForkDefaultBranchMissing),
		errors.Is(err, service.ErrRepoNameTaken), errors.Is(err, service.ErrRepoNameReserved):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, service.ErrInvalidRepoName):
		writeError(w, http.StatusUnprocessableEntity, invalidRepoNameMessage)
		return
	case errors.Is(err, service.ErrInvalidRepoPath):
		slog.Warn("fork repo: unsafe repository path", "owner", req.Owner, "error", err)
		writeError(w, http.StatusUnprocessableEntity, unsafeRepoPathMessage)
		return
	case err != nil:
		slog.Error("fork repo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	repoID := repo.ID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventFork, map[string]any{"fork_owner": forked.OwnerName})

	dest := "/" + forked.OwnerName + "/" + forked.Name
	switch {
	case jsonBody:
		writeJSON(w, http.StatusCreated, map[string]string{"owner": forked.OwnerName, "name": forked.Name, "url": dest})
	case r.Header.Get("HX-Request") == "true":
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(http.StatusOK)
	default:
		http.Redirect(w, r, dest, http.StatusSeeOther)
	}
}

// tokenMayForkInto applies a target-limited token's org list to the namespace
// named in the body: the auth middleware only sees the source in the path.
func tokenMayForkInto(claims middleware.Claims, owner string) bool {
	if len(claims.Targets) == 0 || owner == "" || owner == claims.Username {
		return true
	}
	return slices.ContainsFunc(claims.Targets, func(t string) bool { return strings.EqualFold(t, owner) })
}
