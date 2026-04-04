package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
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

	var body struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Level == "" {
		body.Level = model.WatchLevelWatching
	}

	if err := h.Services.Watch.Watch(r.Context(), owner, repoName, claims.UserID, body.Level); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderWatchButtonFragment(w, r, owner, repoName, claims.UserID)
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

	if err := h.Services.Watch.Unwatch(r.Context(), owner, repoName, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderWatchButtonFragment(w, r, owner, repoName, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetWatchButton handles GET /api/repos/{owner}/{repo}/watch
// Returns the watch-button fragment for HTMX lazy-load.
func (h *Handler) GetWatchButton(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, loggedIn := middleware.ClaimsFromContext(r.Context())
	var userID int64
	if loggedIn {
		userID = claims.UserID
	}
	h.renderWatchButtonFragment(w, r, owner, repoName, userID)
}

func (h *Handler) renderWatchButtonFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, userID int64) {
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	loggedIn := userID != 0
	level := ""
	if loggedIn {
		level = h.Services.Watch.GetLevel(r.Context(), userID, repo.ID)
	}
	h.render(w, r, fragments.WatchButton(view.WatchButtonData{
		Owner:    owner,
		RepoName: repoName,
		RepoID:   repo.ID,
		Level:    level,
		LoggedIn: loggedIn,
	}))
}
