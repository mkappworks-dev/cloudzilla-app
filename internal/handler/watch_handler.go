package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// WatchRepo handles PUT /api/repos/{owner}/{repo}/watch
// Body: {"level": "watching"|"releases_only"|"ignoring"}
func (h *Handler) WatchRepo(w http.ResponseWriter, r *http.Request) {
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

	// hx-vals sends form-encoded parameters, not a JSON body.
	level := r.FormValue("level")
	if level == "" {
		level = model.WatchLevelWatching
	}
	switch level {
	case model.WatchLevelWatching, model.WatchLevelReleasesOnly, model.WatchLevelIgnoring:
	default:
		writeError(w, http.StatusBadRequest, "invalid watch level")
		return
	}

	if err := h.Services.Watch.Watch(r.Context(), owner, repoName, claims.UserID, level); err != nil {
		slog.Error("WatchRepo failed", "owner", owner, "repo", repoName, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update watch preference")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderWatchButtonFragment(w, r, repo.ID, owner, repoName, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UnwatchRepo handles DELETE /api/repos/{owner}/{repo}/watch
func (h *Handler) UnwatchRepo(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Services.Watch.Unwatch(r.Context(), owner, repoName, claims.UserID); err != nil {
		slog.Error("UnwatchRepo failed", "owner", owner, "repo", repoName, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update watch preference")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderWatchButtonFragment(w, r, repo.ID, owner, repoName, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetWatchButton handles GET /api/repos/{owner}/{repo}/watch
// Returns the watch-button fragment for HTMX lazy-load.
func (h *Handler) GetWatchButton(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	var uid int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		uid = claims.UserID
	}
	h.renderWatchButtonFragment(w, r, repo.ID, owner, repoName, uid)
}

func (h *Handler) renderWatchButtonFragment(w http.ResponseWriter, r *http.Request, repoID int64, owner, repoName string, userID int64) {
	loggedIn := userID != 0
	level := ""
	if loggedIn {
		level = h.Services.Watch.GetLevel(r.Context(), userID, repoID)
	}
	h.render(w, r, fragments.WatchButton(view.WatchButtonData{
		Owner:    owner,
		RepoName: repoName,
		RepoID:   repoID,
		Level:    level,
		LoggedIn: loggedIn,
		Count:    h.Services.Watch.CountWatchers(r.Context(), repoID),
	}))
}
