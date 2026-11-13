package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
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
		h.renderLineCommentThread(w, r, repo, owner, repoName, number, view.LineCommentKeyOf(*comment), claims.UserID)
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
		h.renderLineCommentThread(w, r, repo, owner, repoName, number, view.LineCommentKeyOf(*existing), claims.UserID)
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
		h.renderLineCommentThread(w, r, repo, owner, repoName, number, view.LineCommentKeyOf(*comment), claims.UserID)
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
	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
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
	if comment.AppliedAt != nil {
		writeError(w, http.StatusConflict, "this suggestion was already applied")
		return
	}
	if pr.CrossRepo {
		writeError(w, http.StatusUnprocessableEntity, crossRepoUpdateMessage)
		return
	}

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	upd, err := h.Services.Code.ApplySuggestion(
		owner, repoName, pr.HeadBranch, comment.Path,
		comment.Line, comment.SuggestionBody,
		author,
	)
	if err != nil {
		if errors.Is(err, service.ErrRefMoved) {
			writeError(w, http.StatusConflict, branchMovedMsg)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to apply suggestion: "+err.Error())
		return
	}
	h.Services.Push.AfterWebCommit(repo, webCommitActor(claims), upd)
	if _, err := h.Services.PullLineComment.MarkApplied(r.Context(), id, upd.New.String(), claims.UserID, claims.Username); err != nil {
		slog.Warn("apply suggestion: committed but not marked applied; the suggestion will still offer Apply",
			"owner", owner, "repo", repoName, "pull_number", number, "comment_id", id, "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// maxSuggestionBatch bounds one commit's size; more suggestions than this are not a review comment's worth.
const maxSuggestionBatch = 50

// ApplySuggestionBatch commits several suggestions to the head branch as one commit: all of them or none.
func (h *Handler) ApplySuggestionBatch(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}

	var ids []int64
	if isFormEncoded(r) || r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		for _, f := range strings.Split(r.FormValue("ids"), ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	} else {
		var req struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ids = req.IDs
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 || len(ids) > maxSuggestionBatch {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("send between 1 and %d suggestion ids", maxSuggestionBatch))
		return
	}

	var pr *model.PullRequest
	comments := make([]*model.PullLineComment, 0, len(ids))
	edits := make([]service.SuggestionEdit, 0, len(ids))
	for _, id := range ids {
		var c *model.PullLineComment
		pr, c, ok = h.lineCommentOnURLPull(r.Context(), owner, repoName, number, id)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf("comment %d not found", id))
			return
		}
		if !c.IsSuggestion || c.DiffSide != "right" {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("comment %d is not a right-side suggestion", id))
			return
		}
		if c.AppliedAt != nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("suggestion %d was already applied", id))
			return
		}
		comments = append(comments, c)
		edits = append(edits, service.SuggestionEdit{Path: c.Path, Line: c.Line, Replacement: c.SuggestionBody})
	}
	if pr.CrossRepo {
		writeError(w, http.StatusUnprocessableEntity, crossRepoUpdateMessage)
		return
	}
	if pr.State != model.PRStateOpen {
		writeError(w, http.StatusUnprocessableEntity, "cannot apply suggestions to a closed or merged pull request")
		return
	}

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	upd, err := h.Services.Code.ApplySuggestions(owner, repoName, pr.HeadBranch, edits, author)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRefMoved):
			writeError(w, http.StatusConflict, branchMovedMsg)
		case errors.Is(err, service.ErrSuggestionConflict):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to apply suggestions: "+err.Error())
		}
		return
	}
	h.Services.Push.AfterWebCommit(repo, webCommitActor(claims), upd)
	for _, c := range comments {
		if _, err := h.Services.PullLineComment.MarkApplied(r.Context(), c.ID, upd.New.String(), claims.UserID, claims.Username); err != nil {
			slog.Warn("apply suggestions: committed but not marked applied; the suggestion will still offer Apply",
				"owner", owner, "repo", repoName, "pull_number", number, "comment_id", c.ID, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// renderLineCommentThread answers a line-comment write with the thread at key, re-rendered.
func (h *Handler) renderLineCommentThread(w http.ResponseWriter, r *http.Request, repo *model.Repository, owner, repoName string, number int, key view.LineCommentKey, userID int64) {
	allComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	var inThread []model.PullLineComment
	for _, c := range allComments {
		if view.LineCommentKeyOf(c) == key {
			inThread = append(inThread, c)
		}
	}
	pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	h.render(w, h.withAvatars(r, lineCommentAuthors(allComments)...), fragments.LineComments(view.LineCommentsFragData{
		Owner:      owner,
		RepoName:   repoName,
		PullNumber: number,
		Key:        key,
		Comments:   h.lineCommentViews(r.Context(), owner, repoName, pr.HeadRef(), inThread),
		CanWrite:   h.Services.Repo.CanWrite(r.Context(), repo, userID),
		PullOpen:   pr.State == model.PRStateOpen,
	}))
}

// lineCommentViews renders each comment's markdown with its suggestion block taken out of the body, and for a pending
// suggestion reads the head-branch line it would replace.
func (h *Handler) lineCommentViews(ctx context.Context, owner, repoName, headBranch string, comments []model.PullLineComment) []RenderedLineComment {
	files := map[string][]string{}
	views := make([]RenderedLineComment, 0, len(comments))
	for _, c := range comments {
		v := RenderedLineComment{PullLineComment: c, BodyHTML: markdown.RenderCtx(ctx, service.WithoutSuggestion(c.Body))}
		if c.IsSuggestion && c.AppliedAt == nil && c.DiffSide == "right" {
			lines, read := files[c.Path]
			if !read {
				if raw, err := h.Services.Code.GetRawBlob(owner, repoName, headBranch, c.Path); err == nil {
					lines = strings.Split(string(raw), "\n")
				}
				files[c.Path] = lines
			}
			if c.Line >= 1 && c.Line <= len(lines) {
				v.Original, v.HasOriginal = lines[c.Line-1], true
			}
		}
		views = append(views, v)
	}
	return views
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
