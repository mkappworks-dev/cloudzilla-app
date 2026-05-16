package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

type createCommentRequest struct {
	Body string `json:"body"`
}

func (h *Handler) ListIssueComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
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

	repo, repoErr := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repoErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to load repository")
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
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	go func() {
		h.Services.Notification.NotifyIssueComment(r.Context(), *repo, *issue, claims.UserID, claims.Username)
	}()

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.Comment(view.CommentFragData{
			Comment: view.RenderedComment{Comment: *comment, BodyHTML: renderMentionsHTML(markdown.Render(comment.Body))},
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

	repo, repoErr := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repoErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to load repository")
		return
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	comment, err := h.Services.Comment.CreateForPull(r.Context(), *repo, pull.ID, pull.Number, claims.UserID, claims.Username, body)
	if err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	go func() {
		h.Services.Notification.NotifyPRComment(r.Context(), *repo, *pull, claims.UserID, claims.Username)
	}()

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.Comment(view.CommentFragData{
			Comment: view.RenderedComment{Comment: *comment, BodyHTML: renderMentionsHTML(markdown.Render(comment.Body))},
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
			Comment: view.RenderedComment{Comment: *comment, BodyHTML: renderMentionsHTML(markdown.Render(comment.Body))},
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

	existing, err := h.Services.Comment.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	canWrite := repo != nil && h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
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

func (h *Handler) IssueCommentsFragment(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
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
		renderedComments[i] = RenderedComment{Comment: c, BodyHTML: renderMentionsHTML(markdown.Render(c.Body))}
	}
	h.render(w, r, fragments.Comments(view.CommentsFragData{Comments: renderedComments}))
}
