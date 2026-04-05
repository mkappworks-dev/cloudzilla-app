package handler

import (
	"encoding/json"
	"fmt"
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

func (h *Handler) ListLineComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}

	comments, err := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) CreateLineComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}

	var path, diffSide, body string
	var line int

	if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" || r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		path = r.FormValue("path")
		diffSide = r.FormValue("diff_side")
		line, _ = strconv.Atoi(r.FormValue("line"))
		body = r.FormValue("body")
	} else {
		var req struct {
			Path     string `json:"path"`
			DiffSide string `json:"diff_side"`
			Line     int    `json:"line"`
			Body     string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		path = req.Path
		diffSide = req.DiffSide
		line = req.Line
		body = req.Body
	}

	comment, err := h.Services.PullLineComment.Create(r.Context(), owner, repoName, number, claims.UserID, claims.Username, path, diffSide, line, body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		// Re-fetch all comments for this path:line to return updated inline thread
		allComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
		key := fmt.Sprintf("%s:%d", comment.Path, comment.Line)
		var lineComments []RenderedLineComment
		for _, c := range allComments {
			if fmt.Sprintf("%s:%d", c.Path, c.Line) == key {
				lineComments = append(lineComments, RenderedLineComment{
					PullLineComment: c,
					BodyHTML:        markdown.Render(c.Body),
				})
			}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		if repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		h.render(w, r, fragments.LineComments(view.LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Path:       comment.Path,
			Line:       comment.Line,
			Comments:   lineComments,
			CanWrite:   canWrite,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (h *Handler) GetLineCommentForm(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	path := r.URL.Query().Get("path")
	line, _ := strconv.Atoi(r.URL.Query().Get("line"))

	h.render(w, r, fragments.LineCommentForm(view.LineCommentFormFragData{
		Owner:      owner,
		RepoName:   repoName,
		PullNumber: number,
		Path:       path,
		Line:       line,
	}))
}

func (h *Handler) DeleteLineComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	// Fetch comment before deleting so we know path/line for the response
	allBefore, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	var deletedComment *model.PullLineComment
	for _, c := range allBefore {
		if c.ID == id {
			cc := c
			deletedComment = &cc
			break
		}
	}

	// Check: only author or repo writer may delete
	if existing, err := h.Services.PullLineComment.GetComment(r.Context(), id); err == nil {
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := repo != nil && h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		if existing.AuthorID != claims.UserID && !canWrite {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	if err := h.Services.PullLineComment.Delete(r.Context(), owner, repoName, id); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" && deletedComment != nil {
		allComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
		key := fmt.Sprintf("%s:%d", deletedComment.Path, deletedComment.Line)
		var lineComments []RenderedLineComment
		for _, c := range allComments {
			if fmt.Sprintf("%s:%d", c.Path, c.Line) == key {
				lineComments = append(lineComments, RenderedLineComment{
					PullLineComment: c,
					BodyHTML:        markdown.Render(c.Body),
				})
			}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		if repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		h.render(w, r, fragments.LineComments(view.LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Path:       deletedComment.Path,
			Line:       deletedComment.Line,
			Comments:   lineComments,
			CanWrite:   canWrite,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateLineComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	var body string
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		r.ParseForm()
		body = r.FormValue("body")
	} else {
		var req struct {
			Body string `json:"body"`
		}
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

	comment, err := h.Services.PullLineComment.Update(r.Context(), id, claims.UserID, body)
	if err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		allComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
		key := fmt.Sprintf("%s:%d", comment.Path, comment.Line)
		var lineComments []RenderedLineComment
		for _, c := range allComments {
			if fmt.Sprintf("%s:%d", c.Path, c.Line) == key {
				lineComments = append(lineComments, RenderedLineComment{
					PullLineComment: c,
					BodyHTML:        markdown.Render(c.Body),
				})
			}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		if repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		h.render(w, r, fragments.LineComments(view.LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Path:       comment.Path,
			Line:       comment.Line,
			Comments:   lineComments,
			CanWrite:   canWrite,
		}))
		return
	}
	writeJSON(w, http.StatusOK, comment)
}


func (h *Handler) ApplySuggestion(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	comment, err := h.Services.PullLineComment.GetComment(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if !comment.IsSuggestion {
		writeError(w, http.StatusUnprocessableEntity, "comment is not a suggestion")
		return
	}

	pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if pr.State != "open" {
		writeError(w, http.StatusUnprocessableEntity, "cannot apply suggestion to a closed or merged pull request")
		return
	}

	authorEmail := claims.Username + "@localhost"
	if err := h.Services.Code.ApplySuggestion(
		owner, repoName, pr.HeadBranch, comment.Path,
		comment.Line, comment.SuggestionBody,
		claims.Username, authorEmail,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply suggestion: "+err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", fmt.Sprintf("/%s/%s/pulls/%d", owner, repoName, number))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
