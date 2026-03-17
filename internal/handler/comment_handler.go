package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
)

type createCommentRequest struct {
	Body string `json:"body"`
}

func (h *Handler) ListIssueComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	issueNumber, _ := strconv.Atoi(chi.URLParam(r, "number"))

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber)
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

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	comment, err := h.Services.Comment.CreateForIssue(r.Context(), issue.RepoID, issue.ID, claims.UserID, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go func() {
			h.Services.Notification.NotifyIssueComment(r.Context(), *repo, *issue, claims.UserID, claims.Username)
		}()
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-comment", CommentFragData{Comment: *comment})
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
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

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber)
	if err != nil {
		http.Error(w, "issue not found", http.StatusNotFound)
		return
	}

	comments, err := h.Services.Comment.ListByIssue(r.Context(), issue.ID)
	if err != nil {
		http.Error(w, "failed to load comments", http.StatusInternalServerError)
		return
	}

	if comments == nil {
		comments = []model.Comment{}
	}
	h.renderFragment(w, "fragment-comments", CommentsFragData{Comments: comments})
}
