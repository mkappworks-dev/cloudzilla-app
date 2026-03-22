package handler

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
)

func (h *Handler) ListLineComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

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
		h.renderFragment(w, "fragment-line-comments", LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Path:       comment.Path,
			Line:       comment.Line,
			Comments:   lineComments,
			CanWrite:   canWrite,
		})
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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	path := r.URL.Query().Get("path")
	line, _ := strconv.Atoi(r.URL.Query().Get("line"))

	h.renderFragment(w, "fragment-line-comment-form", LineCommentFormFragData{
		Owner:      owner,
		RepoName:   repoName,
		PullNumber: number,
		Path:       path,
		Line:       line,
	})
}

func (h *Handler) DeleteLineComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

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
		h.renderFragment(w, "fragment-line-comments", LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Path:       deletedComment.Path,
			Line:       deletedComment.Line,
			Comments:   lineComments,
			CanWrite:   canWrite,
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// lineCommentID returns a safe HTML ID from path and line number
func lineCommentID(path string, line int) string {
	safe := ""
	for _, ch := range path {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			safe += string(ch)
		} else {
			safe += "-"
		}
	}
	return fmt.Sprintf("lc-%s-%d", safe, line)
}

// RenderedLineComment wraps PullLineComment with pre-rendered HTML body.
type RenderedLineComment struct {
	model.PullLineComment
	BodyHTML template.HTML
}
