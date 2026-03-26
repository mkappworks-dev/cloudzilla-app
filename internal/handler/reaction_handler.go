package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

// ListReactions returns the fragment-reactions snippet for one comment.
func (h *Handler) ListReactions(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	commentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	var userID *int64
	loggedIn := false
	var callerID int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
		callerID = claims.UserID
		loggedIn = true
	}

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	belongs, err := h.Services.Reaction.CommentBelongsToRepo(r.Context(), commentID, repo.ID)
	if err != nil || !belongs {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	reactions, _ := h.Services.Reaction.List(r.Context(), commentID, callerID)
	h.render(w, r, fragments.Reactions(view.ReactionFragData{
		Owner:     owner,
		RepoName:  repoName,
		CommentID: commentID,
		Reactions: reactions,
		LoggedIn:  loggedIn,
	}))
}

// ToggleReaction adds or removes a reaction and returns the updated fragment.
func (h *Handler) ToggleReaction(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	commentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	userID := &claims.UserID
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	belongs, err := h.Services.Reaction.CommentBelongsToRepo(r.Context(), commentID, repo.ID)
	if err != nil || !belongs {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	r.ParseForm()
	emoji := r.FormValue("emoji")

	if _, err := h.Services.Reaction.Toggle(r.Context(), claims.UserID, commentID, emoji); err != nil {
		if strings.HasPrefix(err.Error(), "unsupported emoji") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	reactions, _ := h.Services.Reaction.List(r.Context(), commentID, claims.UserID)
	h.render(w, r, fragments.Reactions(view.ReactionFragData{
		Owner:     owner,
		RepoName:  repoName,
		CommentID: commentID,
		Reactions: reactions,
		LoggedIn:  true,
	}))
}
