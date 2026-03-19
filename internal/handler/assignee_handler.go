package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	username := assigneeUsername(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.AddToIssue(r.Context(), owner, repoName, number, username); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	username := assigneeUsernameDelete(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.RemoveFromIssue(r.Context(), owner, repoName, number, username); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	username := assigneeUsername(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.AddToPull(r.Context(), owner, repoName, number, username); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderPullAssigneeFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemovePullAssignee(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	username := assigneeUsernameDelete(r)

	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if err := h.Services.Assignee.RemoveFromPull(r.Context(), owner, repoName, number, username); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderPullAssigneeFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renderIssueAssigneeFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, issueNumber int) {
	issue, _ := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber)
	var assignees []model.User
	if issue != nil {
		assignees, _ = h.Services.Assignee.GetForIssue(r.Context(), issue.ID)
	}
	if assignees == nil {
		assignees = []model.User{}
	}
	canWrite := false
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
	}
	h.renderFragment(w, "fragment-issue-assignees", IssueAssigneeSidebarData{
		Owner: owner, RepoName: repoName, IssueNumber: issueNumber,
		Assignees: assignees, CanWrite: canWrite,
	})
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
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
	}
	h.renderFragment(w, "fragment-pull-assignees", PullAssigneeSidebarData{
		Owner: owner, RepoName: repoName, PullNumber: pullNumber,
		Assignees: assignees, CanWrite: canWrite,
	})
}
