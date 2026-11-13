package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// A waiting claim first looks for a Job every claimPollEvery and backs off to claimPollMax, so a Job is picked up
// within claimPollMax of being queued while an idle runner costs the database little.
const (
	claimPollEvery = time.Second
	claimPollMax   = 5 * time.Second
)

func bearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[len("Bearer "):])
	}
	return ""
}

// runnerAuth resolves the runner behind the request's token. A bad or revoked token is a 401 with no detail.
func (h *Handler) runnerAuth(w http.ResponseWriter, r *http.Request) (*model.CIRunner, bool) {
	runner, err := h.Services.CIRunner.Authenticate(r.Context(), bearerToken(r))
	if errors.Is(err, service.ErrRunnerUnauthorized) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	return runner, true
}

func decodeRunnerBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// RunnerRegister trades a one-time registration token, sent as the bearer, for a runner token.
func (h *Handler) RunnerRegister(w http.ResponseWriter, r *http.Request) {
	var req runnerapi.RegisterRequest
	if !decodeRunnerBody(w, r, &req) {
		return
	}
	runner, token, err := h.Services.CIRunner.Register(r.Context(), bearerToken(r), req.Name, req.Labels, req.Version)
	switch {
	case errors.Is(err, service.ErrRegistrationToken):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, service.ErrInvalidRunnerLabel):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusCreated, runnerapi.RegisterResponse{RunnerID: runner.ID, Token: token})
	}
}

// RunnerClaim long-polls for a Job: 200 with the Job, or 204 when none turned up within ?wait= seconds. The runner
// is authenticated again on every poll so a revoke ends the wait.
func (h *Handler) RunnerClaim(w http.ResponseWriter, r *http.Request) {
	runner, ok := h.runnerAuth(w, r)
	if !ok {
		return
	}
	var req runnerapi.ClaimRequest
	if r.ContentLength != 0 && !decodeRunnerBody(w, r, &req) {
		return
	}
	// The wait outlives the server's write timeout, which would cut the connection and log EOF at the runner.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	poll := claimPollEvery
	deadline := time.Now().Add(time.Duration(min(max(wait, 0), runnerapi.MaxClaimWaitSeconds)) * time.Second)
	for {
		spec, err := h.Services.CIRunner.Claim(r.Context(), runner, req.Labels)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if spec != nil {
			writeJSON(w, http.StatusOK, spec)
			return
		}
		if !time.Now().Add(poll).Before(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(poll):
		}
		poll = min(poll*2, claimPollMax)
		if runner, ok = h.runnerAuth(w, r); !ok {
			return
		}
	}
}

func (h *Handler) runnerJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrJobNotHeld):
		writeError(w, http.StatusForbidden, "job is not held by this runner")
	case errors.Is(err, service.ErrRunFinished):
		writeError(w, http.StatusConflict, "job has finished")
	case errors.Is(err, service.ErrStepNotFound):
		writeError(w, http.StatusNotFound, "step not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func runnerJobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return 0, false
	}
	return id, true
}

// RunnerHeartbeat renews the Job's lease. continue=false tells the runner to stop: the Job was cancelled or finished.
func (h *Handler) RunnerHeartbeat(w http.ResponseWriter, r *http.Request) {
	runner, ok := h.runnerAuth(w, r)
	if !ok {
		return
	}
	id, ok := runnerJobID(w, r)
	if !ok {
		return
	}
	cont, err := h.Services.CIRunner.Heartbeat(r.Context(), runner, id)
	if err != nil {
		h.runnerJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runnerapi.HeartbeatResponse{Continue: cont})
}

// RunnerStep records the status of one Step of a Job.
func (h *Handler) RunnerStep(w http.ResponseWriter, r *http.Request) {
	runner, ok := h.runnerAuth(w, r)
	if !ok {
		return
	}
	id, ok := runnerJobID(w, r)
	if !ok {
		return
	}
	pos, err := strconv.Atoi(chi.URLParam(r, "position"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid step position")
		return
	}
	var req runnerapi.StepRequest
	if !decodeRunnerBody(w, r, &req) {
		return
	}
	switch model.CIStatus(req.Status) {
	case model.CIStatusRunning, model.CIStatusSuccess, model.CIStatusFailure, model.CIStatusSkipped:
	default:
		writeError(w, http.StatusBadRequest, "status must be running, success, failure or skipped")
		return
	}
	if err := h.Services.CIRunner.SetStep(r.Context(), runner, id, pos, model.CIStatus(req.Status)); err != nil {
		h.runnerJobError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunnerFinish records the result of a Job.
func (h *Handler) RunnerFinish(w http.ResponseWriter, r *http.Request) {
	runner, ok := h.runnerAuth(w, r)
	if !ok {
		return
	}
	id, ok := runnerJobID(w, r)
	if !ok {
		return
	}
	var req runnerapi.FinishRequest
	if !decodeRunnerBody(w, r, &req) {
		return
	}
	switch model.CIStatus(req.Result) {
	case model.CIStatusSuccess, model.CIStatusFailure, model.CIStatusCancelled:
	default:
		writeError(w, http.StatusBadRequest, "result must be success, failure or cancelled")
		return
	}
	if err := h.Services.CIRunner.Finish(r.Context(), runner, id, model.CIStatus(req.Result), req.Message); err != nil {
		h.runnerJobError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// registerCommand is what the runner host runs; labels were checked, so it is safe to paste.
func (h *Handler) registerCommand(token string, labels []string) string {
	return "cz-runner register --server " + h.Cfg.Server.BaseURL + " --token " + token + " --labels " + strings.Join(labels, ",")
}

func (h *Handler) issueRunnerToken(w http.ResponseWriter, r *http.Request, repo *model.Repository, userID int64) {
	labels, err := service.ParseRunnerLabels(r.FormValue("labels"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token, err := h.Services.CIRunner.CreateRegistrationToken(r.Context(), repo, userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.render(w, r, fragments.RunnerToken(view.RunnerTokenFragData{Token: token, Command: h.registerCommand(token, labels), Instance: repo == nil}))
}

// CreateAdminRunnerToken handles POST /admin/runners/tokens: a one-time token for an instance runner.
func (h *Handler) CreateAdminRunnerToken(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())
	h.issueRunnerToken(w, r, nil, claims.UserID)
}

// RevokeAdminRunner handles POST /admin/runners/{id}/revoke.
func (h *Handler) RevokeAdminRunner(w http.ResponseWriter, r *http.Request) {
	runner, ok := h.runnerToRevoke(w, r)
	if !ok || runner.Scope != model.RunnerScopeInstance {
		if ok {
			http.NotFound(w, r)
		}
		return
	}
	if err := h.Services.CIRunner.Revoke(r.Context(), runner.ID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	frag, err := h.adminRunnersFrag(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, fragments.RunnersCard(frag))
}

func (h *Handler) runnerToRevoke(w http.ResponseWriter, r *http.Request) (*model.CIRunner, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	runner, err := h.Services.CIRunner.GetRunner(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	return runner, true
}

// manageableRepo resolves the settings repo for a runner action: unreadable looks missing, a non-manager gets 403.
func (h *Handler) manageableRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, int64, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, 0, false
	}
	repo, ok := h.readableRepo(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), claims.UserID)
	if !ok {
		return nil, 0, false
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, 0, false
	}
	return repo, claims.UserID, true
}

// CreateRepoRunnerToken handles POST /{owner}/{repo}/settings/runners/tokens: a one-time token for a runner that
// serves this repository only.
func (h *Handler) CreateRepoRunnerToken(w http.ResponseWriter, r *http.Request) {
	repo, userID, ok := h.manageableRepo(w, r)
	if !ok {
		return
	}
	h.issueRunnerToken(w, r, repo, userID)
}

// RevokeRepoRunner handles POST /{owner}/{repo}/settings/runners/{id}/revoke.
func (h *Handler) RevokeRepoRunner(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.manageableRepo(w, r)
	if !ok {
		return
	}
	runner, ok := h.runnerToRevoke(w, r)
	if !ok {
		return
	}
	if runner.RepoID == nil || *runner.RepoID != repo.ID {
		http.NotFound(w, r)
		return
	}
	if err := h.Services.CIRunner.Revoke(r.Context(), runner.ID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	frag, err := h.repoRunnersFrag(r.Context(), repo)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, fragments.RunnersCard(frag))
}
