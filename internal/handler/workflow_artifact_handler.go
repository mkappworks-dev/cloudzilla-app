package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
)

func workflowArtifactURL(owner, repo string, runNumber int, id int64) string {
	return "/" + owner + "/" + repo + "/actions/runs/" + strconv.Itoa(runNumber) + "/artifacts/" + strconv.FormatInt(id, 10)
}

// readableRun resolves {owner}/{repo}/runs/{number} for a viewer. A refusal writes nothing: the caller sends one 404,
// which for the web routes is the same whatever the cause, so a private repo can't be told from a missing one.
// repoVisible says the viewer could read the repo, so a JSON caller may then say which part was missing.
func (h *Handler) readableRun(r *http.Request) (repo *model.Repository, run *model.WorkflowRun, repoVisible, ok bool) {
	repo, err := h.Services.Repo.Get(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewerOf(r)) {
		return nil, nil, false, false
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		return nil, nil, true, false
	}
	run, err = h.Services.WorkflowRun.GetRun(r.Context(), repo.ID, number)
	if err != nil {
		return nil, nil, true, false
	}
	return repo, run, true, true
}

// readableJob is readableRun plus the Job, which must belong to the Run.
func (h *Handler) readableJob(r *http.Request) (repo *model.Repository, run *model.WorkflowRun, job *model.WorkflowJob, repoVisible, ok bool) {
	repo, run, repoVisible, ok = h.readableRun(r)
	if !ok {
		return nil, nil, nil, repoVisible, false
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		return nil, nil, nil, true, false
	}
	job, err = h.Services.WorkflowRun.GetJob(r.Context(), id)
	if err != nil || job.RunID != run.ID {
		return nil, nil, nil, true, false
	}
	return repo, run, job, true, true
}

// notFoundJSON is the API's 404: "repo not found" for anything a viewer who can't read the repo asks, so private and
// missing repos look alike, and "not found" for a missing Run or Job in a repo they can read.
func notFoundJSON(w http.ResponseWriter, repoVisible bool) {
	if repoVisible {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusNotFound, "repo not found")
}

// setDownloadHeaders is the release-asset rule set: private, never cached by a shared cache, and inert if a browser
// opens it.
func setDownloadHeaders(w http.ResponseWriter, filename string) {
	hd := w.Header()
	hd.Set("Cache-Control", "private, no-cache")
	hd.Set("Vary", "Authorization, Cookie")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
}

// ListWorkflowArtifacts handles GET /api/repos/{owner}/{repo}/actions/runs/{number}/artifacts.
func (h *Handler) ListWorkflowArtifacts(w http.ResponseWriter, r *http.Request) {
	repo, run, visible, ok := h.readableRun(r)
	if !ok {
		notFoundJSON(w, visible)
		return
	}
	list, err := h.Services.WorkflowLog.ListArtifacts(r.Context(), run.ID)
	if err != nil {
		slog.Error("list artifacts", "run", run.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for i := range list {
		list[i].DownloadURL = workflowArtifactURL(repo.OwnerName, repo.Name, run.Number, list[i].ID)
	}
	if list == nil {
		list = []model.WorkflowArtifact{}
	}
	writeJSON(w, http.StatusOK, list)
}

// DownloadWorkflowArtifact handles GET and HEAD /{owner}/{repo}/actions/runs/{number}/artifacts/{id}. Read access is
// checked on every request, and every refusal, including an unreadable repo, is the same 404 so a private repo's
// artifacts can't be told from missing ones.
func (h *Handler) DownloadWorkflowArtifact(w http.ResponseWriter, r *http.Request) {
	repo, run, _, ok := h.readableRun(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := h.Services.WorkflowLog.GetArtifact(r.Context(), id)
	if errors.Is(err, service.ErrArtifactNotFound) || (err == nil && (a.RunID != run.ID || a.RepoID != repo.ID)) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("artifact: look up", "artifact", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	etag := `"` + a.SHA256 + `"`
	hd := w.Header()
	setDownloadHeaders(w, strings.TrimSuffix(a.Name, ".zip")+".zip")
	hd.Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := h.Services.WorkflowLog.OpenArtifact(r.Context(), a)
	if errors.Is(err, storage.ErrNotFound) {
		hd.Del("ETag")
		hd.Del("Content-Disposition")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("artifact: read object", "artifact", a.ID, "error", err)
		hd.Del("ETag")
		hd.Del("Content-Disposition")
		http.Error(w, "storage unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = body.Close() }()
	extendDeadlines(w)
	hd.Set("Content-Type", "application/zip")
	hd.Set("Content-Length", strconv.FormatInt(a.Size, 10))
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("artifact: stream object", "artifact", a.ID, "error", err)
	}
}
