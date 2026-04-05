package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
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

	var state, body string
	if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" || r.Header.Get("HX-Request") == "true" {
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

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	pr, _ := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if repo != nil && pr != nil {
		go h.Services.Notification.NotifyPRReview(r.Context(), *repo, *pr, claims.UserID, claims.Username)
		go h.tryAutoMerge(owner, repoName, pr.ID)
	}

	if r.Header.Get("HX-Request") == "true" {
		reviews, _ := h.Services.PullReview.ListByPull(r.Context(), owner, repoName, number)
		canMerge, mergeBlockReason, _ := h.Services.PullReview.CanMerge(r.Context(), review.PullID)
		canWrite := false
		if repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		h.render(w, r, fragments.PRReviews(view.PRReviewsFragData{
			Owner:            owner,
			RepoName:         repoName,
			PullNumber:       number,
			Reviews:          reviews,
			CanWrite:         canWrite,
			PullOpen:         pr != nil && pr.State == "open",
			CanMerge:         canMerge,
			MergeBlockReason: mergeBlockReason,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, review)
}
