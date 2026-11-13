package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// actionsErrors maps the ?err= code a refused control redirects back with to the sentence the page shows. The code,
// not free text, travels in the URL so a crafted link cannot put words on the page.
var actionsErrors = map[string]string{
	"not_waiting":   "That run is not waiting for approval.",
	"finished":      "That run has already finished.",
	"not_finished":  "That run is still going. Cancel it first, or wait for it to end.",
	"nothing":       "No job in that run failed, so there is nothing to re-run.",
	"unparseable":   "The workflow file of that run can't be parsed, so it can't run again.",
	"no_workflow":   "That workflow has no workflow_dispatch trigger at this ref, or the file is not there.",
	"bad_ref":       "That branch or tag does not exist.",
	"ci_off":        "CI is off for this repository.",
	"failed":        "That did not work. Try again.",
	"bad_dispatch":  "Choose a workflow and a branch or tag.",
	"bad_rerun_mix": "Choose which jobs to re-run.",
}

func actionsErrorText(r *http.Request) string { return actionsErrors[r.URL.Query().Get("err")] }

// writableRepo resolves the repo of a control: a repo the viewer can't read looks missing (404), a reader gets 403.
func (h *Handler) writableRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, int64, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, 0, false
	}
	repo, ok := h.readableRepo(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return nil, 0, false
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, 0, false
	}
	return repo, claims.UserID, true
}

func actionsRedirect(w http.ResponseWriter, r *http.Request, to, code string) {
	if code != "" {
		to += "?err=" + code
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// controlCode turns a service refusal into a code for actionsErrors.
func controlCode(err error) string {
	switch {
	case errors.Is(err, service.ErrRunNotWaiting):
		return "not_waiting"
	case errors.Is(err, service.ErrRunFinished):
		return "finished"
	case errors.Is(err, service.ErrRunNotFinished):
		return "not_finished"
	case errors.Is(err, service.ErrNothingToRerun):
		return "nothing"
	case errors.Is(err, service.ErrRunNotRerunnable):
		return "unparseable"
	case errors.Is(err, service.ErrWorkflowNotFound), errors.Is(err, service.ErrWorkflowNoDispatch):
		return "no_workflow"
	case errors.Is(err, service.ErrCIDisabled):
		return "ci_off"
	}
	return "failed"
}

// runControl runs fn on the Run in the URL for a writer and sends them back to the Run's page.
func (h *Handler) runControl(w http.ResponseWriter, r *http.Request, fn func(repo *model.Repository, number int, userID int64) error) {
	repo, userID, ok := h.writableRepo(w, r)
	if !ok {
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.Services.WorkflowRun.GetRun(r.Context(), repo.ID, number); err != nil {
		http.NotFound(w, r)
		return
	}
	to := actionsBase(repo) + "/runs/" + chi.URLParam(r, "number")
	if err := fn(repo, number, userID); err != nil {
		code := controlCode(err)
		if code == "failed" {
			slog.Error("actions control failed", "repo_id", repo.ID, "run", number, "error", err)
		}
		actionsRedirect(w, r, to, code)
		return
	}
	actionsRedirect(w, r, to, "")
}

// CancelRun handles POST /{owner}/{repo}/actions/runs/{number}/cancel.
func (h *Handler) CancelRun(w http.ResponseWriter, r *http.Request) {
	h.runControl(w, r, func(repo *model.Repository, n int, u int64) error {
		return h.Services.WorkflowRun.Cancel(r.Context(), repo, n, u)
	})
}

// ApproveRun handles POST /{owner}/{repo}/actions/runs/{number}/approve.
func (h *Handler) ApproveRun(w http.ResponseWriter, r *http.Request) {
	h.runControl(w, r, func(repo *model.Repository, n int, u int64) error {
		return h.Services.WorkflowRun.Approve(r.Context(), repo, n, u)
	})
}

// RerunRun handles POST /{owner}/{repo}/actions/runs/{number}/rerun; failed=1 re-runs only what failed.
func (h *Handler) RerunRun(w http.ResponseWriter, r *http.Request) {
	h.runControl(w, r, func(repo *model.Repository, n int, u int64) error {
		return h.Services.WorkflowRun.Rerun(r.Context(), repo, n, u, r.FormValue("failed") == "1")
	})
}

// DispatchWorkflow handles POST /{owner}/{repo}/actions/dispatch: start a workflow by hand and show its Run.
func (h *Handler) DispatchWorkflow(w http.ResponseWriter, r *http.Request) {
	repo, userID, ok := h.writableRepo(w, r)
	if !ok {
		return
	}
	file, ref := r.FormValue("workflow"), r.FormValue("ref")
	base := actionsBase(repo)
	if file == "" || ref == "" {
		actionsRedirect(w, r, base, "bad_dispatch")
		return
	}
	run, err := h.Services.WorkflowRun.Dispatch(r.Context(), repo, userID, service.WorkflowDir+"/"+file, ref)
	if err != nil {
		code := controlCode(err)
		if code == "failed" && !errors.Is(err, service.ErrWorkflowForbidden) {
			code = "bad_ref"
		}
		actionsRedirect(w, r, base, code)
		return
	}
	actionsRedirect(w, r, base+"/runs/"+strconv.Itoa(run.Number), "")
}
