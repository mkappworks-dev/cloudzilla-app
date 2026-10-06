package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

// mirrorBanner is nil for a repo that isn't a pull mirror.
func (h *Handler) mirrorBanner(ctx context.Context, repo *model.Repository, canWrite, canManage bool) *view.MirrorBanner {
	if !repo.IsMirror {
		return nil
	}
	m, err := h.Services.Mirror.Get(ctx, repo.ID)
	if err != nil {
		slog.Warn("repo page: load mirror failed", "repo_id", repo.ID, "error", err)
		return nil
	}
	b := &view.MirrorBanner{RemoteURL: m.RemoteURL, RemoteLabel: view.MirrorRemoteLabel(m.RemoteURL)}
	if m.LastSyncAt != nil {
		b.SyncedAgo = view.Ago(*m.LastSyncAt)
	}
	if m.LastError != "" {
		b.Failed, b.Error, b.NextTry = true, m.LastError, view.In(m.NextSyncAt)
	}
	path := "/" + repo.OwnerName + "/" + repo.Name
	if canWrite {
		b.SyncURL = "/api/repos" + path + "/mirror/sync"
	}
	if canManage {
		b.SettingsURL = path + "/settings#mirror"
	}
	return b
}

// SyncMirror queues a sync of a pull mirror now.
func (h *Handler) SyncMirror(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, ok := h.writableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return
	}
	if !repo.IsMirror {
		writeError(w, http.StatusNotFound, "repository is not a mirror")
		return
	}
	if err := h.Services.Mirror.SyncNow(r.Context(), repo.ID); err != nil {
		slog.Error("mirror sync now failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not queue the sync")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Sync queued. It starts within a few seconds.")
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
