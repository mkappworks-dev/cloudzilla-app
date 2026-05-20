package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

func (h *Handler) issueWriteContext(w http.ResponseWriter, r *http.Request) (owner, repoName string, number int, repo *model.Repository, userID int64, ok bool) {
	claims, found := middleware.ClaimsFromContext(r.Context())
	if !found {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner = chi.URLParam(r, "owner")
	repoName = chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	repo, err = h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	return owner, repoName, number, repo, claims.UserID, true
}

func (h *Handler) SetIssuePriority(w http.ResponseWriter, r *http.Request) {
	owner, repoName, number, _, _, ok := h.issueWriteContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var priority *string
	switch raw := r.FormValue("priority"); raw {
	case "P0", "P1", "P2", "P3":
		priority = &raw
	case "", "none":
		priority = nil
	default:
		writeError(w, http.StatusBadRequest, "priority must be P0, P1, P2, P3 or empty")
		return
	}

	issue, err := h.Services.Issue.SetPriority(r.Context(), owner, repoName, number, priority)
	if err != nil {
		slog.Error("set issue priority failed", "owner", owner, "repo", repoName, "number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		if issue.Priority != nil {
			toast(w, "success", "Priority set to "+*issue.Priority)
		} else {
			toast(w, "success", "Priority cleared")
		}
		h.render(w, r, fragments.IssuePrioritySidebar(view.IssuePrioritySidebarData{
			Owner:       owner,
			RepoName:    repoName,
			IssueNumber: number,
			Priority:    issue.Priority,
			CanWrite:    true,
		}))
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) IssueTitleSection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	var callerID *int64
	if claims, found := middleware.ClaimsFromContext(r.Context()); found {
		callerID = &claims.UserID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, number, callerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	canWrite := false
	if claims, found := middleware.ClaimsFromContext(r.Context()); found {
		if repo, rerr := h.Services.Repo.Get(r.Context(), owner, repoName); rerr == nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
	}
	h.render(w, r, fragments.IssueTitleSection(owner, repoName, number, issue.Title, canWrite, r.URL.Query().Get("mode") == "edit"))
}

func (h *Handler) EditIssueTitle(w http.ResponseWriter, r *http.Request) {
	owner, repoName, number, repo, userID, ok := h.issueWriteContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	issue, err := h.Services.Issue.EditTitle(r.Context(), owner, repoName, number, title)
	if err != nil {
		if errors.Is(err, service.ErrTitleTooLong) {
			writeError(w, http.StatusBadRequest, "title is too long")
			return
		}
		slog.Error("edit issue title failed", "owner", owner, "repo", repoName, "number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	// Re-derive canWrite from current claims rather than hardcoding true; this
	// matters if permissions were revoked between the gate check and now.
	canWrite := h.Services.Repo.CanWrite(r.Context(), repo, userID)
	h.render(w, r, fragments.IssueTitleSection(owner, repoName, number, issue.Title, canWrite, false))
}

func (h *Handler) IssueBodySection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	var callerID *int64
	if claims, found := middleware.ClaimsFromContext(r.Context()); found {
		callerID = &claims.UserID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, number, callerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	canWrite := false
	if claims, found := middleware.ClaimsFromContext(r.Context()); found {
		if repo, rerr := h.Services.Repo.Get(r.Context(), owner, repoName); rerr == nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
	}
	h.render(w, r, fragments.IssueBodyCard(view.IssueBodyCardData{
		Owner:       owner,
		RepoName:    repoName,
		IssueNumber: number,
		AuthorName:  issue.AuthorName,
		CreatedAt:   issue.CreatedAt,
		Body:        issue.Body,
		BodyHTML:    renderMentionsHTML(markdown.Render(issue.Body)),
		CanWrite:    canWrite,
		Editing:     r.URL.Query().Get("mode") == "edit",
	}))
}

func (h *Handler) EditIssueBody(w http.ResponseWriter, r *http.Request) {
	owner, repoName, number, _, _, ok := h.issueWriteContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issue, err := h.Services.Issue.EditBody(r.Context(), owner, repoName, number, r.FormValue("body"))
	if err != nil {
		slog.Error("edit issue body failed", "owner", owner, "repo", repoName, "number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.render(w, r, fragments.IssueBodyCard(view.IssueBodyCardData{
		Owner:       owner,
		RepoName:    repoName,
		IssueNumber: number,
		AuthorName:  issue.AuthorName,
		CreatedAt:   issue.CreatedAt,
		Body:        issue.Body,
		BodyHTML:    renderMentionsHTML(markdown.Render(issue.Body)),
		CanWrite:    true,
		Editing:     false,
	}))
}

func (h *Handler) LinkIssuePull(w http.ResponseWriter, r *http.Request) {
	h.setIssuePullLink(w, r, true)
}

func (h *Handler) UnlinkIssuePull(w http.ResponseWriter, r *http.Request) {
	h.setIssuePullLink(w, r, false)
}

func (h *Handler) setIssuePullLink(w http.ResponseWriter, r *http.Request, link bool) {
	owner, repoName, issueNumber, _, userID, ok := h.issueWriteContext(w, r)
	if !ok {
		return
	}
	pullNumber, err := strconv.Atoi(chi.URLParam(r, "pullNumber"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, &userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if link {
		err = h.Services.Issue.LinkPull(r.Context(), pull.ID, issue.ID)
	} else {
		err = h.Services.Issue.UnlinkPull(r.Context(), pull.ID, issue.ID)
	}
	if err != nil {
		slog.Error("set issue pull link failed", "owner", owner, "repo", repoName,
			"issue_number", issueNumber, "pull_number", pullNumber, "link", link, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		if link {
			toast(w, "success", "Pull request linked")
		} else {
			toast(w, "success", "Pull request unlinked")
		}
		h.renderIssueLinkedPullsFragment(w, r, owner, repoName, issueNumber, true)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renderIssueLinkedPullsFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, issueNumber int, canWrite bool) {
	linked, err := h.Services.Issue.LinkedPRs(r.Context(), owner, repoName, issueNumber)
	if err != nil {
		slog.Warn("issue linked pulls fragment: linked list failed", "owner", owner, "repo", repoName, "error", err)
	}
	all, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("issue linked pulls fragment: repo PR list failed", "owner", owner, "repo", repoName, "error", err)
	}
	h.render(w, r, fragments.IssueLinkedPullsSidebar(view.IssueLinkedPullsSidebarData{
		Owner:       owner,
		RepoName:    repoName,
		IssueNumber: issueNumber,
		Linked:      pullsToLinkedPulls(linked),
		AllPulls:    pullsToLinkedPulls(all),
		CanWrite:    canWrite,
	}))
}

func pullsToLinkedPulls(pulls []model.PullRequest) []view.LinkedPull {
	out := make([]view.LinkedPull, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, view.LinkedPull{Number: p.Number, Title: p.Title, State: string(p.State)})
	}
	return out
}
