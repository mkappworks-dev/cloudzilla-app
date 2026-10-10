package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// CreateDiscussion handles POST /api/repos/{owner}/{repo}/discussions
func (h *Handler) CreateDiscussion(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	authRepo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	if !authRepo.AllowDiscussions {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var body struct {
		CategoryID int64  `json:"category_id"`
		Title      string `json:"title"`
		Body       string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	d, err := h.Services.Discussion.Create(r.Context(), owner, repoName, claims.UserID, claims.Username, body.CategoryID, body.Title, body.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// CreateReply handles POST /api/repos/{owner}/{repo}/discussions/{number}/replies
func (h *Handler) CreateReply(w http.ResponseWriter, r *http.Request) {
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
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	if discussion.IsLocked && !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "discussion is locked")
		return
	}

	hxRequest := r.Header.Get("HX-Request") == "true"

	var replyBody string
	var parentID *int64
	if hxRequest {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		replyBody = r.FormValue("body")
		if pid := r.FormValue("parent_id"); pid != "" {
			if v, err := strconv.ParseInt(pid, 10, 64); err == nil {
				parentID = &v
			}
		}
	} else {
		var body struct {
			Body     string `json:"body"`
			ParentID *int64 `json:"parent_id,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		replyBody, parentID = body.Body, body.ParentID
	}

	reply, err := h.Services.Discussion.CreateReply(r.Context(), *discussion, claims.UserID, claims.Username, replyBody, parentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	go h.Services.Notification.NotifyDiscussionReply(context.WithoutCancel(r.Context()), *repo, *discussion, claims.UserID, claims.Username)

	if hxRequest {
		canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		allReplies, _ := h.Services.Discussion.ListReplies(r.Context(), discussion.ID)
		rendered := view.RenderedDiscussionReply{
			DiscussionReply: *reply,
			BodyHTML:        markdown.RenderCtx(r.Context(), reply.Body),
		}
		h.render(w, h.withAvatars(r, reply.AuthorName), pages.DiscussionReplyCreated(owner, repoName, number, rendered, len(allReplies), canWrite, true))
		return
	}
	writeJSON(w, http.StatusCreated, reply)
}

// MarkAnswer handles PATCH /api/repos/{owner}/{repo}/discussions/{number}
// Body: {"answer_id": 123} to mark, or {"answer_id": null} to clear.
func (h *Handler) MarkAnswer(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
		return
	}

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	var body struct {
		AnswerID   json.RawMessage `json:"answer_id"`
		Locked     *bool           `json:"locked"`
		Title      *string         `json:"title"`
		Body       *string         `json:"body"`
		CategoryID *int64          `json:"category_id"`
	}
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		if r.Form.Has("answer_id") {
			// hx-vals serializes JSON null to the literal string "null".
			v := r.FormValue("answer_id")
			if v == "" {
				v = "null"
			}
			body.AnswerID = json.RawMessage(v)
		}
		if r.Form.Has("locked") {
			locked := r.FormValue("locked") == "true"
			body.Locked = &locked
		}
		if r.Form.Has("title") {
			title := r.FormValue("title")
			body.Title = &title
		}
		if r.Form.Has("body") {
			text := r.FormValue("body")
			body.Body = &text
		}
		if r.Form.Has("category_id") {
			v, err := strconv.ParseInt(r.FormValue("category_id"), 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid category id")
				return
			}
			body.CategoryID = &v
		}
	} else if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}
		// Length cap is enforced by DiscussionService.UpdateContent.
		body.Title = &title
	}
	if body.CategoryID != nil {
		cat, cerr := h.Services.Discussion.GetCategory(r.Context(), *body.CategoryID)
		if cerr != nil {
			slog.Error("mark answer: category lookup failed", "discussion", discussion.ID, "category", *body.CategoryID, "error", cerr)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if cat == nil {
			writeError(w, http.StatusUnprocessableEntity, "unknown category")
			return
		}
	}

	if body.AnswerID != nil {
		if string(body.AnswerID) == "null" {
			if err := h.Services.Discussion.SetAnswer(r.Context(), discussion.ID, nil); err != nil {
				slog.Error("operation failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
		} else {
			var replyID int64
			if err := json.Unmarshal(body.AnswerID, &replyID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid answer_id")
				return
			}
			if err := h.Services.Discussion.SetAnswer(r.Context(), discussion.ID, &replyID); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if body.Locked != nil {
		if err := h.Services.Discussion.Lock(r.Context(), discussion.ID, *body.Locked); err != nil {
			slog.Error("operation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}
	if body.Title != nil || body.Body != nil {
		title := discussion.Title
		if body.Title != nil {
			title = *body.Title
		}
		bodyText := discussion.Body
		if body.Body != nil {
			bodyText = *body.Body
		}
		if err := h.Services.Discussion.UpdateContent(r.Context(), discussion.ID, title, bodyText); err != nil {
			if errors.Is(err, service.ErrTitleTooLong) {
				writeError(w, http.StatusBadRequest, "title is too long")
				return
			}
			slog.Error("mark answer: update content failed", "discussion", discussion.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}
	if body.CategoryID != nil {
		if err := h.Services.Discussion.SetCategory(r.Context(), discussion.ID, *body.CategoryID); err != nil {
			slog.Error("operation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteDiscussionReply handles DELETE /api/repos/{owner}/{repo}/discussions/{number}/replies/{id}
func (h *Handler) DeleteDiscussionReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}
	idStr := chi.URLParam(r, "id")
	replyID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reply id")
		return
	}

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
		return
	}

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	if err := h.Services.Discussion.DeleteReply(r.Context(), replyID, discussion.ID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
