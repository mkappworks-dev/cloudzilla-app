package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

type createCommentRequest struct {
	Body string `json:"body"`
}

func (h *Handler) ListIssueComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	issueNumber, _ := strconv.Atoi(chi.URLParam(r, "number"))

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
	issueNumber, _ := strconv.Atoi(chi.URLParam(r, "number"))

	var body string
	if r.Header.Get("HX-Request") == "true" {
		r.ParseForm()
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
		writeError(w, http.StatusInternalServerError, err.Error())
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

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, _ := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)

	var body string
	if r.Header.Get("HX-Request") == "true" {
		r.ParseForm()
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
	id, _ := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)

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
	issueNumber, _ := strconv.Atoi(chi.URLParam(r, "number"))

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
