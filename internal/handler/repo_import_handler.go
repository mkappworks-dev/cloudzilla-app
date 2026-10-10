package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type startImportRequest struct {
	CloneURL     string `json:"clone_url"`
	AuthUsername string `json:"auth_username"`
	AuthToken    string `json:"auth_token"`
	Owner        string `json:"owner"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Private      bool   `json:"private"`
	Mirror       bool   `json:"mirror"`
	// A Go duration such as "8h"; empty means mirror.default_interval.
	MirrorInterval string `json:"mirror_interval"`
}

type importJobResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Progress  string `json:"progress,omitempty"`
	Error     string `json:"error,omitempty"`
	StatusURL string `json:"status_url"`
}

func importJobJSON(j service.ImportJob) importJobResponse {
	return importJobResponse{
		ID: j.ID, Status: string(j.Status), Owner: j.Owner, Name: j.Name,
		Progress: j.Progress, Error: j.Error, StatusURL: "/repos/import/" + j.ID,
	}
}

func (h *Handler) StartImport(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req startImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var interval time.Duration
	if req.Mirror && req.MirrorInterval != "" {
		d, err := time.ParseDuration(req.MirrorInterval)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, `mirror_interval must be a duration such as "8h"`)
			return
		}
		interval = d
	}
	job, err := h.Services.Import.Start(r.Context(), claims.UserID, claims.Username, service.ImportRequest{
		CloneURL:       req.CloneURL,
		AuthUsername:   req.AuthUsername,
		AuthToken:      req.AuthToken,
		Owner:          req.Owner,
		Name:           req.Name,
		Description:    req.Description,
		Private:        req.Private,
		Mirror:         req.Mirror,
		MirrorInterval: interval,
	})
	switch {
	case errors.Is(err, service.ErrQuotaReached):
		writeError(w, http.StatusForbidden, err.Error())
		return
	case errors.Is(err, service.ErrImportURL), errors.Is(err, service.ErrImportURLUserinfo),
		errors.Is(err, service.ErrImportCredentials),
		errors.Is(err, service.ErrRepoNameTaken), errors.Is(err, service.ErrRepoNameReserved),
		errors.Is(err, service.ErrMirrorsDisabled), errors.Is(err, service.ErrMirrorInterval), errors.Is(err, service.ErrMirrorNoSecretKey):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, service.ErrInvalidRepoName):
		writeError(w, http.StatusUnprocessableEntity, invalidRepoNameMessage)
		return
	case errors.Is(err, service.ErrForbidden):
		writeError(w, http.StatusForbidden, "you can import only into your account or an organization you own")
		return
	case errors.Is(err, service.ErrTooManyImports):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case err != nil:
		slog.Error("start repo import", "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start the import")
		return
	}

	// Recorded at start, not on success: a refused request to a private address belongs in the log.
	action, meta := model.AuditActionRepoImport, map[string]any{"owner": job.Owner, "source_url": job.SourceURL, "job_id": job.ID}
	if req.Mirror {
		action, meta["interval"] = model.AuditActionRepoMirrorCreate, req.MirrorInterval
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, action, model.AuditTargetRepo, 0, job.Name, meta)
	writeJSON(w, http.StatusAccepted, importJobJSON(job))
}

func (h *Handler) GetImportJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	job, err := h.Services.Import.Get(claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "import not found")
		return
	}
	writeJSON(w, http.StatusOK, importJobJSON(job))
}
