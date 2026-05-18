package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

func assigneeUsername(r *http.Request) string {
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		_ = r.ParseForm()
		return r.FormValue("username")
	}
	// fallback: JSON body
	var body struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body.Username
}

func assigneeUsernameDelete(r *http.Request) string {
	username := r.URL.Query().Get("username")
	if username == "" {
		_ = r.ParseForm()
		username = r.FormValue("username")
	}
	return username
}

func (h *Handler) AddIssueAssignee(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	username := assigneeUsername(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.AddToIssue(r.Context(), owner, repoName, number, username); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderIssueAssigneeFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveIssueAssignee(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	username := assigneeUsernameDelete(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.RemoveFromIssue(r.Context(), owner, repoName, number, username); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderIssueAssigneeFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddPullAssignee(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	username := assigneeUsername(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.AddToPull(r.Context(), owner, repoName, number, username); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.recordPullAssigneeEvent(r, owner, repoName, number, username, model.PullEventAssigned)

	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Assignee added")
		h.renderPullAssigneeFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemovePullAssignee(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	username := assigneeUsernameDelete(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.RemoveFromPull(r.Context(), owner, repoName, number, username); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.recordPullAssigneeEvent(r, owner, repoName, number, username, model.PullEventUnassigned)

	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Assignee removed")
		h.renderPullAssigneeFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// recordPullAssigneeEvent appends an assigned/unassigned entry to the PR timeline.
func (h *Handler) recordPullAssigneeEvent(r *http.Request, owner, repoName string, number int, username, eventType string) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return
	}
	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		slog.Warn("record pull assignee event: pull lookup failed; timeline entry skipped",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
		return
	}
	h.Services.PullEvent.Record(r.Context(), pull.ID, claims.UserID, claims.Username, eventType, username)
}

func (h *Handler) renderIssueAssigneeFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, issueNumber int) {
	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
	}
	issue, _ := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, callerID)
	var assignees []model.User
	if issue != nil {
		assignees, _ = h.Services.Assignee.GetForIssue(r.Context(), issue.ID)
	}
	if assignees == nil {
		assignees = []model.User{}
	}
	canWrite := false
	var collaborators []model.Permission
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		collaborators, _ = h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	}
	h.render(w, r, fragments.IssueAssignees(view.IssueAssigneeSidebarData{
		Owner: owner, RepoName: repoName, IssueNumber: issueNumber,
		Assignees: assignees, Collaborators: collaboratorUsernames(collaborators), CanWrite: canWrite,
	}))
}

func (h *Handler) renderPullAssigneeFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, pullNumber int) {
	pull, _ := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	var assignees []model.User
	if pull != nil {
		assignees, _ = h.Services.Assignee.GetForPull(r.Context(), pull.ID)
	}
	if assignees == nil {
		assignees = []model.User{}
	}
	canWrite := false
	var collaborators []model.Permission
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		collaborators, _ = h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	}
	h.render(w, r, fragments.PullAssignees(view.PullAssigneeSidebarData{
		Owner: owner, RepoName: repoName, PullNumber: pullNumber,
		Assignees: assignees, Collaborators: collaboratorUsernames(collaborators), CanWrite: canWrite,
	}))
}
