package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type createStatusRequest struct {
	State       model.CommitStatusState `json:"state"`
	Context     string                  `json:"context"`
	TargetURL   string                  `json:"target_url"`
	Description string                  `json:"description"`
}

func (h *Handler) CreateStatus(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")

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
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req createStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.State {
	case model.CommitStatusPending, model.CommitStatusSuccess, model.CommitStatusFailure, model.CommitStatusError:
	default:
		writeError(w, http.StatusBadRequest, "state must be pending, success, failure, or error")
		return
	}

	if req.TargetURL != "" {
		u, err := url.Parse(req.TargetURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			writeError(w, http.StatusBadRequest, "target_url must be an http or https URL")
			return
		}
	}

	cs := &model.CommitStatus{
		State:       req.State,
		Context:     req.Context,
		TargetURL:   req.TargetURL,
		Description: req.Description,
		CreatorID:   claims.UserID,
	}

	if err := h.Services.CommitStatus.Upsert(r.Context(), owner, repoName, sha, cs); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Kick off auto-merge for any open PR whose head branch tip matches this SHA.
	go func(owner, repoName, sha string) {
		ctx := context.Background()
		openPRs, err := h.Services.Pull.ListOpen(ctx, owner, repoName)
		if err != nil {
			return
		}
		for _, pr := range openPRs {
			if !pr.AutoMergeEnabled {
				continue
			}
			_, headSHA, err := h.Services.Code.ResolveRef(owner, repoName, pr.HeadBranch)
			if err != nil || headSHA != sha {
				continue
			}
			h.tryAutoMerge(owner, repoName, pr.ID)
		}
	}(owner, repoName, sha)

	writeJSON(w, http.StatusCreated, cs)
}

func (h *Handler) ListStatuses(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")

	statuses, err := h.Services.CommitStatus.List(r.Context(), owner, repoName, sha)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if statuses == nil {
		statuses = []model.CommitStatus{}
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (h *Handler) GetCombinedStatus(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")

	combined, statuses, err := h.Services.CommitStatus.GetCombined(r.Context(), owner, repoName, sha)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if statuses == nil {
		statuses = []model.CommitStatus{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":    string(combined),
		"statuses": statuses,
	})
}
