package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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

// mirrorJSON never carries the token, only whether one is stored.
type mirrorJSON struct {
	RemoteURL           string     `json:"remote_url"`
	AuthUsername        string     `json:"auth_username"`
	HasToken            bool       `json:"has_token"`
	Interval            string     `json:"interval"`
	NextSyncAt          time.Time  `json:"next_sync_at"`
	LastSyncAt          *time.Time `json:"last_sync_at"`
	LastSuccessAt       *time.Time `json:"last_success_at"`
	LastError           string     `json:"last_error"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

func mirrorResponse(m *model.RepoMirror) mirrorJSON {
	return mirrorJSON{
		RemoteURL: m.RemoteURL, AuthUsername: m.AuthUsername, HasToken: m.AuthTokenEnc != nil,
		Interval: m.Interval.String(), NextSyncAt: m.NextSyncAt, LastSyncAt: m.LastSyncAt,
		LastSuccessAt: m.LastSuccessAt, LastError: m.LastError, ConsecutiveFailures: m.ConsecutiveFailures,
	}
}

type updateMirrorRequest struct {
	RemoteURL    *string `json:"remote_url"`
	AuthUsername *string `json:"auth_username"`
	AuthToken    *string `json:"auth_token"`
	ClearToken   bool    `json:"clear_token"`
	Interval     *string `json:"interval"`
}

// mirrorRepo is the manageable mirror the request names, or false once it has answered.
func (h *Handler) mirrorRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, middleware.Claims, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, claims, false
	}
	repo, ok := h.manageableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return nil, claims, false
	}
	if !repo.IsMirror {
		writeError(w, http.StatusNotFound, "repository is not a mirror")
		return nil, claims, false
	}
	return repo, claims, true
}

func (h *Handler) GetMirror(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	m, err := h.Services.Mirror.Get(r.Context(), repo.ID)
	if err != nil {
		slog.Error("get mirror failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, mirrorResponse(m))
}

// UpdateMirror takes JSON, or the settings form, where an empty token keeps
// the stored one.
func (h *Handler) UpdateMirror(w http.ResponseWriter, r *http.Request) {
	repo, claims, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	var req updateMirrorRequest
	if isJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		for field, dst := range map[string]**string{"remote_url": &req.RemoteURL, "auth_username": &req.AuthUsername, "auth_token": &req.AuthToken, "interval": &req.Interval} {
			if r.PostForm.Has(field) {
				v := r.PostForm.Get(field)
				*dst = &v
			}
		}
		req.ClearToken = r.PostForm.Get("clear_token") != ""
	}

	update := service.MirrorUpdate{RemoteURL: req.RemoteURL, AuthUsername: req.AuthUsername, AuthToken: req.AuthToken, ClearToken: req.ClearToken}
	if req.Interval != nil {
		d, err := time.ParseDuration(*req.Interval)
		if err != nil {
			h.mirrorFormError(w, r, `interval must be a duration such as "8h"`)
			return
		}
		update.Interval = &d
	}
	m, err := h.Services.Mirror.Update(r.Context(), repo.ID, update)
	switch {
	case errors.Is(err, service.ErrImportURL), errors.Is(err, service.ErrImportURLUserinfo), errors.Is(err, service.ErrImportCredentials),
		errors.Is(err, service.ErrMirrorInterval), errors.Is(err, service.ErrMirrorNoSecretKey):
		h.mirrorFormError(w, r, err.Error())
		return
	case err != nil:
		slog.Error("update mirror failed", "repo_id", repo.ID, "error", err)
		settingsError(w, r, http.StatusInternalServerError, "could not save the mirror settings")
		return
	}

	var changed []string
	for field, set := range map[string]bool{"remote_url": req.RemoteURL != nil, "auth_username": req.AuthUsername != nil,
		"auth_token": req.AuthToken != nil && *req.AuthToken != "" || req.ClearToken, "interval": req.Interval != nil} {
		if set {
			changed = append(changed, field)
		}
	}
	slices.Sort(changed)
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoMirrorUpdate,
		model.AuditTargetRepo, repo.ID, repo.OwnerName+"/"+repo.Name, map[string]any{"fields": changed, "remote_url": m.RemoteURL})
	if r.Header.Get("HX-Request") == "true" {
		redirectToRepoSettings(w, r, repo.OwnerName, repo.Name)
		return
	}
	writeJSON(w, http.StatusOK, mirrorResponse(m))
}

func (h *Handler) mirrorFormError(w http.ResponseWriter, r *http.Request, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		renderFormError(w, "#mirror-form-error", msg)
		return
	}
	writeError(w, http.StatusUnprocessableEntity, msg)
}

// StopMirror turns the mirror into a regular repository.
func (h *Handler) StopMirror(w http.ResponseWriter, r *http.Request) {
	repo, claims, ok := h.mirrorRepo(w, r)
	if !ok {
		return
	}
	if err := h.Services.Mirror.Stop(r.Context(), repo.ID); err != nil {
		slog.Error("stop mirror failed", "repo_id", repo.ID, "error", err)
		settingsError(w, r, http.StatusInternalServerError, "could not stop mirroring")
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoMirrorDelete,
		model.AuditTargetRepo, repo.ID, repo.OwnerName+"/"+repo.Name, nil)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/"+repo.OwnerName+"/"+repo.Name)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mirrorSettings is nil for a repo that isn't a pull mirror.
func (h *Handler) mirrorSettings(ctx context.Context, repo *model.Repository) *view.MirrorSettings {
	if !repo.IsMirror {
		return nil
	}
	m, err := h.Services.Mirror.Get(ctx, repo.ID)
	if err != nil {
		slog.Warn("settings: load mirror failed", "repo_id", repo.ID, "error", err)
		return nil
	}
	path := "/" + repo.OwnerName + "/" + repo.Name
	s := &view.MirrorSettings{
		RemoteURL: m.RemoteURL, AuthUsername: m.AuthUsername, HasToken: m.AuthTokenEnc != nil,
		LastSynced: "never", NextSync: view.In(m.NextSyncAt), Failing: m.LastError != "", LastError: m.LastError,
		APIURL: "/api/repos" + path + "/mirror", SyncURL: "/api/repos" + path + "/mirror/sync",
	}
	if m.LastSyncAt != nil {
		s.LastSynced = view.Ago(*m.LastSyncAt)
	}
	current := m.Interval.String()
	for _, c := range h.Services.Mirror.IntervalChoices() {
		s.Intervals = append(s.Intervals, view.IntervalOption{Value: c.Value, Label: c.Label, Selected: c.Value == current})
	}
	if !slices.ContainsFunc(s.Intervals, func(o view.IntervalOption) bool { return o.Selected }) {
		s.Intervals = append([]view.IntervalOption{{Value: current, Label: service.FormatMirrorInterval(m.Interval), Selected: true}}, s.Intervals...)
	}
	return s
}
