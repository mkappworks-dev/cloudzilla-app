package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

type createCommentRequest struct {
	Body string `json:"body"`
}

// commentEventBody trims a comment body to a short snippet for the activity feed.
func commentEventBody(body string) string {
	const max = 280
	r := []rune(strings.TrimSpace(body))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

func (h *Handler) ListIssueComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	issueNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, callerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	comments, err := h.Services.Comment.ListByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) CreateIssueComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	issueNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var body string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		body = r.FormValue("body")
	} else {
		var req createCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		body = req.Body
	}

	if strings.TrimSpace(body) == "" {
		writeError(w, http.StatusBadRequest, "body required")
		return
	}

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, &claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	// Enforce lock: non-managers cannot comment on locked issues.
	if issue.IsLocked {
		if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
			writeError(w, http.StatusForbidden, "issue is locked")
			return
		}
	}

	comment, err := h.Services.Comment.CreateForIssue(r.Context(), *repo, issue.ID, issue.Number, claims.UserID, claims.Username, body)
	if err != nil {
		slog.Error("create issue comment: store create failed",
			"owner", owner, "repo", repoName, "issue_number", issueNumber, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	concurrency.Go("notify.issue_comment", func() {
		h.Services.Notification.NotifyIssueComment(context.WithoutCancel(r.Context()), *repo, *issue, claims.UserID, claims.Username)
	})

	repoID := repo.ID
	concurrency.Go("event.record.issue_comment", func() {
		h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventComment, map[string]any{
			"number": issue.Number,
			"kind":   "issue",
			"body":   commentEventBody(body),
		})
	})

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.Comment(view.CommentFragData{
			Comment: view.RenderedComment{Comment: *comment, BodyHTML: renderMentionsHTML(markdown.RenderCtx(r.Context(), comment.Body))},
		}))
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (h *Handler) CreatePullComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	pullNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}

	var body string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		body = r.FormValue("body")
	} else {
		var req createCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		body = req.Body
	}

	if strings.TrimSpace(body) == "" {
		writeError(w, http.StatusBadRequest, "body required")
		return
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	comment, err := h.Services.Comment.CreateForPull(r.Context(), *repo, pull.ID, pull.Number, claims.UserID, claims.Username, body)
	if err != nil {
		slog.Error("create pull comment: store create failed",
			"owner", owner, "repo", repoName, "pull_number", pullNumber, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	concurrency.Go("notify.pr_comment", func() {
		h.Services.Notification.NotifyPRComment(context.WithoutCancel(r.Context()), *repo, *pull, claims.UserID, claims.Username)
	})

	repoID := repo.ID
	concurrency.Go("event.record.pr_comment", func() {
		h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventComment, map[string]any{
			"number": pull.Number,
			"kind":   "pull",
			"body":   commentEventBody(body),
		})
	})

	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Comment added")
		h.render(w, r, fragments.Comment(view.CommentFragData{
			Comment: view.RenderedComment{Comment: *comment, BodyHTML: renderMentionsHTML(markdown.RenderCtx(r.Context(), comment.Body))},
		}))
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	var body string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		body = r.FormValue("body")
	} else {
		var req createCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		body = req.Body
	}

	if strings.TrimSpace(body) == "" {
		writeError(w, http.StatusBadRequest, "body required")
		return
	}

	// The comment must belong to the repo in the URL so the path identifies a
	// single comment unambiguously.
	if existing, err := h.Services.Comment.GetByID(r.Context(), id); err != nil || existing.RepoID != repo.ID || !h.commentUnderURL(r, existing, claims.UserID) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	comment, err := h.Services.Comment.Update(r.Context(), id, claims.UserID, body)
	if err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.Comment(view.CommentFragData{
			Comment: view.RenderedComment{Comment: *comment, BodyHTML: renderMentionsHTML(markdown.RenderCtx(r.Context(), comment.Body))},
		}))
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	// The comment must belong to the repo in the URL — otherwise write access to
	// any repo would authorize deleting comments in repos the caller cannot see.
	existing, err := h.Services.Comment.GetByID(r.Context(), id)
	if err != nil || existing.RepoID != repo.ID || !h.commentUnderURL(r, existing, claims.UserID) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	if existing.AuthorID != claims.UserID && !canWrite {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.Services.Comment.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// commentUnderURL reports whether c belongs to the issue or pull request the URL
// names. UpdateComment and DeleteComment serve both route trees, and the OAuth
// scope gate relies on the path to tell issue comments from pull request comments.
func (h *Handler) commentUnderURL(r *http.Request, c *model.Comment, viewerID int64) bool {
	owner, repoName := chi.URLParam(r, "owner"), chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		return false
	}
	if strings.Contains(chi.RouteContext(r.Context()).RoutePattern(), "/pulls/") {
		pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		return err == nil && c.PullID != nil && *c.PullID == pr.ID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, number, &viewerID)
	return err == nil && c.IssueID != nil && *c.IssueID == issue.ID
}

func (h *Handler) IssueCommentsFragment(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	issueNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, callerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	comments, err := h.Services.Comment.ListByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load comments")
		return
	}

	if comments == nil {
		comments = []model.Comment{}
	}
	renderedComments := make([]RenderedComment, len(comments))
	for i, c := range comments {
		renderedComments[i] = RenderedComment{Comment: c, BodyHTML: renderMentionsHTML(markdown.RenderCtx(r.Context(), c.Body))}
	}
	h.render(w, r, fragments.Comments(view.CommentsFragData{Comments: renderedComments}))
}
