package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
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

	cs := &model.CommitStatus{
		State:       req.State,
		Context:     req.Context,
		TargetURL:   req.TargetURL,
		Description: req.Description,
		CreatorID:   claims.UserID,
	}

	if err := h.Services.CommitStatus.Upsert(r.Context(), owner, repoName, sha, cs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
