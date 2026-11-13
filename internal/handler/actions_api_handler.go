package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const (
	apiRunsPageSize    = 20
	apiRunsMaxPageSize = 100
)

type apiJob struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Context    string     `json:"context"`
	Status     string     `json:"status"`
	Message    string     `json:"message,omitempty"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type apiRun struct {
	ID           int64      `json:"id"`
	Number       int        `json:"number"`
	WorkflowPath string     `json:"workflow_path"`
	WorkflowName string     `json:"workflow_name"`
	Event        string     `json:"event"`
	Ref          string     `json:"ref"`
	SHA          string     `json:"sha"`
	Status       string     `json:"status"`
	Attempt      int        `json:"attempt"`
	Error        string     `json:"error,omitempty"`
	HTMLURL      string     `json:"html_url"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Jobs         []apiJob   `json:"jobs,omitempty"`
}

func (h *Handler) apiRunOf(repo *model.Repository, run *model.WorkflowRun, jobs []model.WorkflowJob) apiRun {
	out := apiRun{
		ID: run.ID, Number: run.Number, WorkflowPath: run.WorkflowPath, WorkflowName: run.WorkflowName, Event: run.Event,
		Ref: runRefName(run.Ref), SHA: run.SHA, Status: string(run.Status), Attempt: run.Attempt, Error: run.Error,
		HTMLURL:   h.Cfg.Server.BaseURL + actionsBase(repo) + "/runs/" + strconv.Itoa(run.Number),
		CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
	for _, j := range jobs {
		out.Jobs = append(out.Jobs, apiJob{ID: j.ID, Name: j.Name, Context: j.StatusContext, Status: string(j.Status), Message: j.Message,
			StartedAt: j.StartedAt, FinishedAt: j.FinishedAt})
	}
	return out
}

// apiActionsError answers a refused control with a status a script can branch on.
func apiActionsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrWorkflowForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrRunNotWaiting), errors.Is(err, service.ErrRunFinished), errors.Is(err, service.ErrRunNotFinished),
		errors.Is(err, service.ErrCIDisabled):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNothingToRerun), errors.Is(err, service.ErrRunNotRerunnable),
		errors.Is(err, service.ErrWorkflowNotFound), errors.Is(err, service.ErrWorkflowNoDispatch):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		slog.Error("actions api", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// ListWorkflowRuns handles GET /api/repos/{owner}/{repo}/actions/runs?workflow=&branch=&status=&event=&page=&per_page=.
func (h *Handler) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.readableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := model.RunFilter{WorkflowPath: q.Get("workflow"), Branch: q.Get("branch"), Status: model.CIStatus(q.Get("status")), Event: q.Get("event")}
	if filter.Status != "" && !validRunStatus(filter.Status) {
		writeError(w, http.StatusBadRequest, "status must be one of waiting, queued, running, success, failure, cancelled")
		return
	}
	if filter.Event != "" && !slicesContains(runFilterEvents, filter.Event) {
		writeError(w, http.StatusBadRequest, "event must be one of push, pull_request, manual")
		return
	}
	page, err := queryInt(r, "page", 1)
	if err != nil || page < 1 {
		writeError(w, http.StatusBadRequest, "page must be a positive number")
		return
	}
	size, err := queryInt(r, "per_page", apiRunsPageSize)
	if err != nil || size < 1 || size > apiRunsMaxPageSize {
		writeError(w, http.StatusBadRequest, "per_page must be between 1 and "+strconv.Itoa(apiRunsMaxPageSize))
		return
	}
	runs, more, err := h.Services.WorkflowRun.ListRunsPage(r.Context(), repo.ID, filter, page, size)
	if err != nil {
		slog.Error("actions api: list runs", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]apiRun, 0, len(runs))
	for i := range runs {
		out = append(out, h.apiRunOf(repo, &runs[i], nil))
	}
	w.Header().Set("Vary", "Authorization, Cookie")
	writeJSON(w, http.StatusOK, map[string]any{"runs": out, "has_more": more})
}

func validRunStatus(s model.CIStatus) bool {
	for _, ok := range runFilterStatuses {
		if s == ok {
			return true
		}
	}
	return false
}

func slicesContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// GetWorkflowRun handles GET /api/repos/{owner}/{repo}/actions/runs/{number}.
func (h *Handler) GetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	repo, run, visible, ok := h.readableRun(r)
	if !ok {
		notFoundJSON(w, visible)
		return
	}
	h.writeRun(w, r, repo, run, http.StatusOK)
}

func (h *Handler) writeRun(w http.ResponseWriter, r *http.Request, repo *model.Repository, run *model.WorkflowRun, status int) {
	jobs, err := h.Services.WorkflowRun.ListJobs(r.Context(), run.ID)
	if err != nil {
		slog.Error("actions api: list jobs", "run_id", run.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Vary", "Authorization, Cookie")
	writeJSON(w, status, h.apiRunOf(repo, run, jobs))
}

// controlWorkflowRun runs fn on the Run in the URL for a writer and answers with the Run as it then is.
func (h *Handler) controlWorkflowRun(w http.ResponseWriter, r *http.Request, fn func(repo *model.Repository, number int, userID int64) error) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, ok := h.writableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := h.Services.WorkflowRun.GetRun(r.Context(), repo.ID, number); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := fn(repo, number, claims.UserID); err != nil {
		apiActionsError(w, err)
		return
	}
	run, err := h.Services.WorkflowRun.GetRun(r.Context(), repo.ID, number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.writeRun(w, r, repo, run, http.StatusOK)
}

// CancelWorkflowRun handles POST /api/repos/{owner}/{repo}/actions/runs/{number}/cancel.
func (h *Handler) CancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	h.controlWorkflowRun(w, r, func(repo *model.Repository, n int, u int64) error {
		return h.Services.WorkflowRun.Cancel(r.Context(), repo, n, u)
	})
}

// RerunWorkflowRun handles POST /api/repos/{owner}/{repo}/actions/runs/{number}/rerun; the optional body
// {"failed_only": true} re-runs only what failed.
func (h *Handler) RerunWorkflowRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FailedOnly bool `json:"failed_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	h.controlWorkflowRun(w, r, func(repo *model.Repository, n int, u int64) error {
		return h.Services.WorkflowRun.Rerun(r.Context(), repo, n, u, in.FailedOnly)
	})
}

// DispatchWorkflowRun handles POST /api/repos/{owner}/{repo}/actions/dispatch with {"workflow": "ci.yml", "ref": "main"}.
func (h *Handler) DispatchWorkflowRun(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, ok := h.writableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return
	}
	var in struct {
		Workflow string `json:"workflow"`
		Ref      string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Workflow == "" || in.Ref == "" {
		writeError(w, http.StatusBadRequest, "workflow (a file name under "+service.WorkflowDir+") and ref are required")
		return
	}
	run, err := h.Services.WorkflowRun.Dispatch(r.Context(), repo, claims.UserID, service.WorkflowDir+"/"+in.Workflow, in.Ref)
	if err != nil {
		if errors.Is(err, service.ErrRefNotFound) || !isWorkflowError(err) {
			writeError(w, http.StatusUnprocessableEntity, "unknown branch or tag")
			return
		}
		apiActionsError(w, err)
		return
	}
	h.writeRun(w, r, repo, run, http.StatusCreated)
}

func isWorkflowError(err error) bool {
	for _, target := range []error{service.ErrWorkflowForbidden, service.ErrCIDisabled, service.ErrWorkflowNotFound, service.ErrWorkflowNoDispatch} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
