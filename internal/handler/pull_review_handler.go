package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
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

	reviews, err := h.Services.PullReview.ListByPull(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, reviews)
}

func (h *Handler) SubmitReview(w http.ResponseWriter, r *http.Request) {
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

	repo, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	var state, body string
	if isFormEncoded(r) || r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		state = r.FormValue("state")
		body = r.FormValue("body")
	} else {
		var req struct {
			State string `json:"state"`
			Body  string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = req.State
		body = req.Body
	}

	review, err := h.Services.PullReview.SubmitReview(r.Context(), owner, repoName, number, claims.UserID, claims.Username, state, body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pr, _ := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if state == string(model.PRReviewCommented) && review.State != model.PRReviewCommented && pr != nil && repo != nil {
		// The reviewer's standing verdict was kept, so the note goes to the timeline as a plain comment.
		if _, err := h.Services.Comment.CreateForPull(r.Context(), *repo, pr.ID, pr.Number, claims.UserID, claims.Username, body); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to post comment")
			return
		}
	}
	if repo != nil && pr != nil {
		go h.Services.Notification.NotifyPRReview(context.WithoutCancel(r.Context()), *repo, *pr, claims.UserID, claims.Username)
		go h.tryAutoMerge(owner, repoName, pr.ID)
	}

	if r.Header.Get("HX-Request") == "true" {
		// The page reloads itself (see the review dialog's hx-on), so there is no fragment to swap.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusCreated, review)
}

// DismissReview clears a reviewer's request for changes. Repo admins and owners only: writers can merge, so they must
// not be able to remove the check on themselves.
func (h *Handler) DismissReview(w http.ResponseWriter, r *http.Request) {
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
	reviewID, err := strconv.ParseInt(chi.URLParam(r, "reviewID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid review id")
		return
	}
	if _, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
		return
	}

	var reason string
	if isFormEncoded(r) || r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		reason = r.FormValue("reason")
	} else {
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		reason = req.Reason
	}

	review, err := h.Services.PullReview.Dismiss(r.Context(), owner, repoName, number, reviewID, claims.Username, reason)
	switch {
	case errors.Is(err, service.ErrReviewNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, service.ErrReviewNotDismissible):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionPullReviewDismiss,
		model.AuditTargetPull, review.PullID, owner+"/"+repoName+"#"+strconv.Itoa(number),
		map[string]any{"review_id": review.ID, "reviewer": review.AuthorName, "reason": review.DismissedReason})
	go h.tryAutoMerge(owner, repoName, review.PullID)

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, review)
}
