package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// workflowLogResponse is a window of a Job's log. Complete means the Job is over, so a reader that has caught up can
// stop polling.
type workflowLogResponse struct {
	model.WorkflowLogPage
	Complete bool `json:"complete"`
}

// GetWorkflowJobLog handles GET /api/repos/{owner}/{repo}/actions/runs/{number}/jobs/{jobID}/logs?offset=&limit=.
// A viewer who can't read the repo gets the 404 of a missing repo.
func (h *Handler) GetWorkflowJobLog(w http.ResponseWriter, r *http.Request) {
	_, _, job, visible, ok := h.readableJob(r)
	if !ok {
		notFoundJSON(w, visible)
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil || offset < 0 {
		writeError(w, http.StatusBadRequest, "offset must be a non-negative line number")
		return
	}
	limit, err := queryInt(r, "limit", service.DefaultLogLimit)
	if err != nil || limit < 1 {
		writeError(w, http.StatusBadRequest, "limit must be a positive number of lines")
		return
	}
	page, err := h.Services.WorkflowLog.ReadLog(r.Context(), job.ID, offset, limit)
	if err != nil {
		slog.Error("read job log", "job", job.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Vary", "Authorization, Cookie")
	writeJSON(w, http.StatusOK, workflowLogResponse{WorkflowLogPage: *page, Complete: job.Status.Finished()})
}

func queryInt(r *http.Request, key string, def int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	return strconv.Atoi(v)
}

// DownloadWorkflowJobLog handles GET /{owner}/{repo}/actions/runs/{number}/jobs/{jobID}/log: the whole log as text.
func (h *Handler) DownloadWorkflowJobLog(w http.ResponseWriter, r *http.Request) {
	_, _, job, _, ok := h.readableJob(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	setDownloadHeaders(w, "job-"+strconv.FormatInt(job.ID, 10)+".log")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	extendDeadlines(w)
	if err := h.Services.WorkflowLog.WriteLog(r.Context(), job.ID, w); err != nil {
		slog.Warn("download job log", "job", job.ID, "error", err)
	}
}
