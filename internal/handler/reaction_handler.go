package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
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

	reactions, lerr := h.Services.Reaction.List(r.Context(), commentID, callerID)
	if lerr != nil {
		slog.Warn("list reactions: reload failed; bar may render empty", "comment", commentID, "error", lerr)
	}
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

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	emoji := r.FormValue("emoji")

	if _, err := h.Services.Reaction.Toggle(r.Context(), claims.UserID, commentID, emoji); err != nil {
		if strings.HasPrefix(err.Error(), "unsupported emoji") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	reactions, lerr := h.Services.Reaction.List(r.Context(), commentID, claims.UserID)
	if lerr != nil {
		// Toggle succeeded but the re-fetch failed — render with the stale empty
		// slice rather than dropping the response; double-toggle on the next
		// click is preferable to leaving the user staring at nothing.
		slog.Warn("toggle reaction: reload failed", "comment", commentID, "error", lerr)
	}
	h.render(w, r, fragments.Reactions(view.ReactionFragData{
		Owner:     owner,
		RepoName:  repoName,
		CommentID: commentID,
		Reactions: reactions,
		LoggedIn:  true,
	}))
}

func (h *Handler) ToggleDiscussionReaction(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, &claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if _, err := h.Services.Reaction.ToggleDiscussion(r.Context(), claims.UserID, discussion.ID, r.FormValue("emoji")); err != nil {
		if strings.HasPrefix(err.Error(), "unsupported emoji") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	reactions, lerr := h.Services.Reaction.ListByDiscussion(r.Context(), discussion.ID, claims.UserID)
	if lerr != nil {
		slog.Warn("toggle discussion reaction: reload failed", "discussion", discussion.ID, "error", lerr)
	}
	endpoint := "/api/repos/" + owner + "/" + repoName + "/discussions/" + strconv.Itoa(number) + "/reactions"
	h.render(w, r, fragments.ReactionBar(endpoint, "reactions-discussion-"+strconv.Itoa(number), reactions, true))
}

func (h *Handler) ToggleDiscussionReplyReaction(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}
	replyID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reply id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, &claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}
	replies, rerr := h.Services.Discussion.ListReplies(r.Context(), discussion.ID)
	if rerr != nil {
		// We can't confirm reply membership; fail closed.
		slog.Error("toggle discussion reply reaction: list replies failed", "discussion", discussion.ID, "error", rerr)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	found := false
	for _, rp := range replies {
		if rp.ID == replyID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "reply not found")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if _, err := h.Services.Reaction.ToggleReply(r.Context(), claims.UserID, replyID, r.FormValue("emoji")); err != nil {
		if strings.HasPrefix(err.Error(), "unsupported emoji") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	reactions, lerr := h.Services.Reaction.ListByReply(r.Context(), replyID, claims.UserID)
	if lerr != nil {
		slog.Warn("toggle discussion reply reaction: reload failed", "reply", replyID, "error", lerr)
	}
	endpoint := "/api/repos/" + owner + "/" + repoName + "/discussions/" + strconv.Itoa(number) + "/replies/" + strconv.FormatInt(replyID, 10) + "/reactions"
	h.render(w, r, fragments.ReactionBar(endpoint, "reactions-reply-"+strconv.FormatInt(replyID, 10), reactions, true))
}
