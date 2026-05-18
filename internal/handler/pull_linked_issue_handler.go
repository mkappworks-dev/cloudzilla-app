package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// LinkPullIssue links an issue to a pull request via the sidebar picker.
func (h *Handler) LinkPullIssue(w http.ResponseWriter, r *http.Request) {
	h.setPullIssueLink(w, r, true)
}

// UnlinkPullIssue removes an issue ↔ pull-request link.
func (h *Handler) UnlinkPullIssue(w http.ResponseWriter, r *http.Request) {
	h.setPullIssueLink(w, r, false)
}

func (h *Handler) setPullIssueLink(w http.ResponseWriter, r *http.Request, link bool) {
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

	pullNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	issueNumber, err := strconv.Atoi(chi.URLParam(r, "issueNumber"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, &claims.UserID)
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
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		if link {
			toast(w, "success", "Issue linked")
		} else {
			toast(w, "success", "Issue unlinked")
		}
		h.renderLinkedIssuesFragment(w, r, owner, repoName, pull.ID, pullNumber, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renderLinkedIssuesFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, pullID int64, pullNumber int, userID int64) {
	linked, _ := h.Services.Issue.LinkedForPull(r.Context(), pullID)
	all, _ := h.Services.Issue.List(r.Context(), owner, repoName, &userID)
	canWrite := false
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, userID)
	}
	h.render(w, r, fragments.LinkedIssuesSidebar(view.LinkedIssuesSidebarData{
		Owner:      owner,
		RepoName:   repoName,
		PullNumber: pullNumber,
		Linked:     linkedIssuesToView(linked),
		AllIssues:  linkedIssuesToView(all),
		CanWrite:   canWrite,
	}))
}

// linkedIssuesToView converts issue models to the lightweight linked-issue view struct.
func linkedIssuesToView(issues []model.Issue) []view.LinkedIssue {
	out := make([]view.LinkedIssue, 0, len(issues))
	for _, i := range issues {
		out = append(out, view.LinkedIssue{Number: i.Number, Title: i.Title, State: string(i.State)})
	}
	return out
}
