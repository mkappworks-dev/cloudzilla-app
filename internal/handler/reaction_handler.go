package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
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

	var callerID int64
	loggedIn := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = claims.UserID
		loggedIn = true
	}

	reactions, _ := h.Services.Reaction.List(r.Context(), commentID, callerID)
	h.renderFragment(w, "fragment-reactions", ReactionFragData{
		Owner:     owner,
		RepoName:  repoName,
		CommentID: commentID,
		Reactions: reactions,
		LoggedIn:  loggedIn,
	})
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

	r.ParseForm()
	emoji := r.FormValue("emoji")

	if _, err := h.Services.Reaction.Toggle(r.Context(), claims.UserID, commentID, emoji); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	reactions, _ := h.Services.Reaction.List(r.Context(), commentID, claims.UserID)
	h.renderFragment(w, "fragment-reactions", ReactionFragData{
		Owner:     owner,
		RepoName:  repoName,
		CommentID: commentID,
		Reactions: reactions,
		LoggedIn:  true,
	})
}
