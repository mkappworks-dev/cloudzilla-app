package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// jobTokenAuth resolves the per-job token of a log or artifact request and checks that it is for the Job in the URL.
// A token that has died with its Job is a 401, one for another Job a 403.
func (h *Handler) jobTokenAuth(w http.ResponseWriter, r *http.Request) (*service.JobToken, bool) {
	id, ok := runnerJobID(w, r)
	if !ok {
		return nil, false
	}
	tok, err := h.Services.CIRunner.VerifyJobToken(r.Context(), bearerToken(r))
	if errors.Is(err, service.ErrRunnerUnauthorized) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if tok.JobID != id {
		writeError(w, http.StatusForbidden, "token is for another job")
		return nil, false
	}
	return tok, true
}

// RunnerPostLog handles POST /api/runner/jobs/{id}/log?step=&seq=: the body is whole lines of text. The per-job token
// authenticates it, so a Job can write only its own log.
func (h *Handler) RunnerPostLog(w http.ResponseWriter, r *http.Request) {
	tok, ok := h.jobTokenAuth(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	seq, err := strconv.Atoi(q.Get("seq"))
	if err != nil || seq < 0 {
		writeError(w, http.StatusBadRequest, "seq must be a non-negative integer")
		return
	}
	step := -1
	if v := q.Get("step"); v != "" {
		if step, err = strconv.Atoi(v); err != nil || step < -1 {
			writeError(w, http.StatusBadRequest, "step must be a Step position")
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, runnerapi.MaxLogChunkBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "a log chunk can be at most 1 MiB")
		return
	}
	truncated, err := h.Services.WorkflowLog.AppendLog(r.Context(), tok, step, seq, body)
	switch {
	case errors.Is(err, service.ErrStorageUnconfigured):
		writeError(w, http.StatusServiceUnavailable, "storage is unavailable")
	case err != nil:
		slog.Error("log chunk failed", "job", tok.JobID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, runnerapi.LogResponse{Truncated: truncated})
	}
}

// RunnerUploadArtifact handles POST /api/runner/jobs/{id}/artifacts?name=: the body is the zip archive. The runner
// uploads before it finishes the Job, since the token dies with it.
func (h *Handler) RunnerUploadArtifact(w http.ResponseWriter, r *http.Request) {
	tok, ok := h.jobTokenAuth(w, r)
	if !ok {
		return
	}
	extendDeadlines(w)
	a, err := h.Services.WorkflowLog.UploadArtifact(r.Context(), tok, r.URL.Query().Get("name"), r.Body)
	var tooBig *http.MaxBytesError
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, runnerapi.ArtifactResponse{ID: a.ID, Name: a.Name, Size: a.Size})
	case errors.As(err, &tooBig), errors.Is(err, service.ErrArtifactTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, tooLargeMessage(err, h))
	case errors.Is(err, service.ErrArtifactLimit):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrArtifactNameTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrArtifactName):
		writeError(w, http.StatusBadRequest, "artifact name is empty")
	case errors.Is(err, service.ErrStorageUnconfigured):
		writeError(w, http.StatusServiceUnavailable, "storage is unavailable")
	default:
		slog.Error("artifact upload failed", "job", tok.JobID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func tooLargeMessage(err error, h *Handler) string {
	var le *service.LimitError
	if errors.As(err, &le) {
		return le.Msg
	}
	return "an artifact can be at most " + byteSize(h.Services.WorkflowLog.MaxArtifactBytes()) + " (ci.max_artifact_bytes)"
}
