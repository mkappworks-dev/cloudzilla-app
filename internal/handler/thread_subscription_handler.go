package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// SetIssueSubscription handles PUT /api/repos/{owner}/{repo}/issues/{number}/subscription
func (h *Handler) SetIssueSubscription(w http.ResponseWriter, r *http.Request) {
	h.setThreadSubscription(w, r, model.ThreadKindIssue, "issues")
}

// SetPullSubscription handles PUT /api/repos/{owner}/{repo}/pulls/{number}/subscription
func (h *Handler) SetPullSubscription(w http.ResponseWriter, r *http.Request) {
	h.setThreadSubscription(w, r, model.ThreadKindPull, "pulls")
}

// SetDiscussionSubscription handles PUT /api/repos/{owner}/{repo}/discussions/{number}/subscription
func (h *Handler) SetDiscussionSubscription(w http.ResponseWriter, r *http.Request) {
	h.setThreadSubscription(w, r, model.ThreadKindDiscussion, "discussions")
}

func (h *Handler) setThreadSubscription(w http.ResponseWriter, r *http.Request, kind, segment string) {
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
	if err != nil || !h.threadExists(r, kind, owner, repoName, number) {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}

	state, ok := threadSubscriptionState(w, r)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, `state must be "subscribed" or "muted"`)
		return
	}
	if err := h.Services.ThreadSubscription.Set(r.Context(), claims.UserID, repo.ID, kind, int64(number), state); err != nil {
		slog.Error("set thread subscription failed", "owner", owner, "repo", repoName, "kind", kind, "number", number, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update subscription")
		return
	}

	data, err := h.threadSubscriptionData(r, repo, kind, segment, number)
	if err != nil {
		slog.Error("read thread subscription failed", "owner", owner, "repo", repoName, "kind", kind, "number", number, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read subscription")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.ThreadSubscription(data))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": data.State, "reason": data.Reason})
}

// threadSubscriptionState reads state from a JSON body, or from the form that hx-vals sends.
func threadSubscriptionState(w http.ResponseWriter, r *http.Request) (string, bool) {
	var state string
	if isFormEncoded(r) {
		state = r.FormValue("state")
	} else {
		var body struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
			return "", false
		}
		state = body.State
	}
	if state != model.ThreadStateSubscribed && state != model.ThreadStateMuted {
		return "", false
	}
	return state, true
}

func (h *Handler) threadExists(r *http.Request, kind, owner, repoName string, number int) bool {
	var err error
	switch kind {
	case model.ThreadKindIssue:
		_, err = h.Services.Issue.Get(r.Context(), owner, repoName, number, viewerOf(r))
	case model.ThreadKindPull:
		_, err = h.Services.Pull.Get(r.Context(), owner, repoName, number)
	case model.ThreadKindDiscussion:
		_, err = h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	}
	return err == nil
}

// threadSubscriptionData is the viewer's control state for a thread. Anonymous viewers get the zero state, which the pages don't render.
func (h *Handler) threadSubscriptionData(r *http.Request, repo *model.Repository, kind, segment string, number int) (view.ThreadSubscriptionData, error) {
	data := view.ThreadSubscriptionData{Owner: repo.OwnerName, RepoName: repo.Name, Segment: segment, Number: number}
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return data, nil
	}
	st, err := h.Services.ThreadSubscription.Status(r.Context(), claims.UserID, repo.ID, kind, int64(number))
	if err != nil {
		return data, err
	}
	data.State, data.Reason = st.State, st.Reason
	return data, nil
}
