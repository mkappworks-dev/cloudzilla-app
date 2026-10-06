package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (h *Handler) ListLineComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}

	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
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

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	var path, diffSide, body string
	var line int

	if isFormEncoded(r) || r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		path = r.FormValue("path")
		diffSide = r.FormValue("diff_side")
		line, _ = strconv.Atoi(r.FormValue("line"))
		if line <= 0 {
			writeError(w, http.StatusBadRequest, "invalid line number")
			return
		}
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
		allComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
		key := view.LineCommentKeyOf(*comment)
		var lineComments []RenderedLineComment
		for _, c := range allComments {
			if view.LineCommentKeyOf(c) == key {
				lineComments = append(lineComments, RenderedLineComment{
					PullLineComment: c,
					BodyHTML:        markdown.RenderCtx(r.Context(), c.Body),
				})
			}
		}
		canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		h.render(w, r, fragments.LineComments(view.LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Key:        key,
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
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	path := r.URL.Query().Get("path")
	line, _ := strconv.Atoi(r.URL.Query().Get("line"))
	if line <= 0 {
		writeError(w, http.StatusBadRequest, "invalid line number")
		return
	}

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
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
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

	_, existing, ok := h.lineCommentOnURLPull(r.Context(), owner, repoName, number, id)
	if !ok {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if existing.AuthorID != claims.UserID && !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.Services.PullLineComment.Delete(r.Context(), owner, repoName, id); err != nil {
		slog.Error("delete line comment failed", "owner", owner, "repo", repoName, "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		allComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
		key := view.LineCommentKeyOf(*existing)
		var lineComments []RenderedLineComment
		for _, c := range allComments {
			if view.LineCommentKeyOf(c) == key {
				lineComments = append(lineComments, RenderedLineComment{
					PullLineComment: c,
					BodyHTML:        markdown.RenderCtx(r.Context(), c.Body),
				})
			}
		}
		canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		h.render(w, r, fragments.LineComments(view.LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Key:        key,
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
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
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
	if r.Header.Get("HX-Request") == "true" || isFormEncoded(r) {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
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

	if _, _, ok := h.lineCommentOnURLPull(r.Context(), owner, repoName, number, id); !ok {
		writeError(w, http.StatusNotFound, "comment not found")
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
		key := view.LineCommentKeyOf(*comment)
		var lineComments []RenderedLineComment
		for _, c := range allComments {
			if view.LineCommentKeyOf(c) == key {
				lineComments = append(lineComments, RenderedLineComment{
					PullLineComment: c,
					BodyHTML:        markdown.RenderCtx(r.Context(), c.Body),
				})
			}
		}
		canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		h.render(w, r, fragments.LineComments(view.LineCommentsFragData{
			Owner:      owner,
			RepoName:   repoName,
			PullNumber: number,
			Key:        key,
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
	if _, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
		return
	}
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

	pr, comment, ok := h.lineCommentOnURLPull(r.Context(), owner, repoName, number, id)
	if !ok {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if !comment.IsSuggestion {
		writeError(w, http.StatusUnprocessableEntity, "comment is not a suggestion")
		return
	}
	// A left-side line numbers the base file, but applying edits the head branch.
	if comment.DiffSide != "right" {
		writeError(w, http.StatusUnprocessableEntity, "only right-side suggestions can be applied")
		return
	}
	if pr.State != "open" {
		writeError(w, http.StatusUnprocessableEntity, "cannot apply suggestion to a closed or merged pull request")
		return
	}

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if err := h.Services.Code.ApplySuggestion(
		owner, repoName, pr.HeadBranch, comment.Path,
		comment.Line, comment.SuggestionBody,
		author,
	); err != nil {
		if errors.Is(err, service.ErrRefMoved) {
			writeError(w, http.StatusConflict, branchMovedMsg)
			return
		}
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

// lineCommentOnURLPull loads the pull request the URL names and line comment id,
// provided the comment is on that pull request. Comment IDs are global, so without
// this check access to one repo would reach line comments in any other.
func (h *Handler) lineCommentOnURLPull(ctx context.Context, owner, repoName string, number int, id int64) (*model.PullRequest, *model.PullLineComment, bool) {
	pr, err := h.Services.Pull.Get(ctx, owner, repoName, number)
	if err != nil {
		return nil, nil, false
	}
	c, err := h.Services.PullLineComment.GetComment(ctx, id)
	if err != nil || c.RepoID != pr.RepoID || c.PullID != pr.ID {
		return nil, nil, false
	}
	return pr, c, true
}
