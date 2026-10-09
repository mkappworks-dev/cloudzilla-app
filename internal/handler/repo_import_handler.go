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
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
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

func (h *Handler) PageImportRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	orgs, err := h.Services.Org.ListOwnedByUser(ctx, claims.UserID)
	if err != nil {
		slog.Error("list owned orgs", "error", err)
		orgs = []model.Organization{}
	}
	q := r.URL.Query()
	// As on /repos/new, ?owner= is honored only for an org the viewer owns.
	owner, private := claims.Username, false
	for _, o := range orgs {
		if o.Name == q.Get("owner") {
			owner, private = o.Name, o.DefaultRepoVisibility != "public"
			break
		}
	}
	h.render(w, r, pages.RepoImport(view.RepoImportData{
		BasePage:        withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(ctx, claims.UserID)),
		OwnedOrgs:       orgs,
		DefaultOwner:    owner,
		DefaultPrivate:  private,
		DefaultURL:      q.Get("url"),
		DefaultName:     q.Get("name"),
		MirrorIntervals: h.mirrorIntervalOptions(),
	}))
}

func (h *Handler) mirrorIntervalOptions() []view.IntervalOption {
	if !h.Services.Mirror.Enabled() {
		return nil
	}
	var opts []view.IntervalOption
	for _, c := range h.Services.Mirror.IntervalChoices() {
		opts = append(opts, view.IntervalOption{Value: c.Value, Label: c.Label, Selected: c.Default})
	}
	return opts
}

func (h *Handler) PageImportStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	job, err := h.Services.Import.Get(claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	repoURL := "/" + job.Owner + "/" + job.Name
	if r.Header.Get("HX-Request") == "true" {
		if job.Status == service.ImportDone {
			w.Header().Set("HX-Redirect", repoURL)
		}
		h.render(w, r, pages.RepoImportStatusPanel(job))
		return
	}
	if job.Status == service.ImportDone {
		http.Redirect(w, r, repoURL, http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.RepoImportStatus(view.RepoImportStatusData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(r.Context(), claims.UserID)),
		Job:      job,
	}))
}
