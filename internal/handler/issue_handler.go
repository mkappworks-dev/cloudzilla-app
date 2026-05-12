package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

type createIssueRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	Visibility string `json:"visibility"`
}

type updateIssueRequest struct {
	State string `json:"state"`
}

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
	}
	issues, err := h.Services.Issue.List(r.Context(), owner, repo, callerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repo, number, callerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	var req createIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	vis := req.Visibility
	if vis != "private" {
		vis = "public"
	}
	issue, err := h.Services.Issue.Create(r.Context(), owner, repoName, claims.UserID, req.Title, req.Body, vis)
	if err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload("opened", *repo, *issue))
		repoID := repo.ID
		go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventIssueOpened, map[string]any{"number": issue.Number, "title": issue.Title})
	}

	writeJSON(w, http.StatusCreated, issue)
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var state string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = r.FormValue("state")
	} else {
		var req updateIssueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = req.State
	}

	if state != "open" && state != "closed" {
		writeError(w, http.StatusBadRequest, "state must be 'open' or 'closed'")
		return
	}

	issue, err := h.Services.Issue.SetState(r.Context(), owner, repoName, number, model.IssueState(state))
	if err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload(state, *repo, *issue))
	go func() {
		h.Services.Notification.NotifyIssueStateChange(r.Context(), *repo, *issue, claims.UserID, claims.Username)
	}()
	if state == "closed" {
		repoID := repo.ID
		go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventIssueClosed, map[string]any{"number": issue.Number})
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.IssueDetail(view.IssueDetailFragData{
			Issue: *issue, Owner: owner, Repo: repoName,
			BodyHTML: markdown.Render(issue.Body),
		}))
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

// PinIssue handles PATCH /api/repos/{owner}/{repo}/issues/{number}/pin
func (h *Handler) PinIssue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var action string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		action = r.FormValue("action")
	} else {
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		action = req.Action
	}

	if action == "unpin" {
		err = h.Services.Issue.UnpinIssue(r.Context(), owner, repoName, number, claims.UserID)
	} else {
		err = h.Services.Issue.PinIssue(r.Context(), owner, repoName, number, claims.UserID)
	}
	if err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LockIssue handles PATCH /api/repos/{owner}/{repo}/issues/{number}/lock
func (h *Handler) LockIssue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var action string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		action = r.FormValue("action")
	} else {
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		action = req.Action
	}

	if action == "unlock" {
		err = h.Services.Issue.UnlockIssue(r.Context(), owner, repoName, number, claims.UserID)
	} else {
		err = h.Services.Issue.LockIssue(r.Context(), owner, repoName, number, claims.UserID)
	}
	if err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
