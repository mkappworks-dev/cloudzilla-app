package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

type createIssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type updateIssueRequest struct {
	State string `json:"state"`
}

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	issues, err := h.Services.Issue.List(r.Context(), owner, repo)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	issue, err := h.Services.Issue.Get(r.Context(), owner, repo, number)
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
	issue, err := h.Services.Issue.Create(r.Context(), owner, repoName, claims.UserID, req.Title, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload("opened", *repo, *issue))
	}

	writeJSON(w, http.StatusCreated, issue)
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

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

	issue, err := h.Services.Issue.SetState(r.Context(), owner, repoName, number, model.IssueState(state))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload(state, *repo, *issue))
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			go func() {
				h.Services.Notification.NotifyIssueStateChange(r.Context(), *repo, *issue, claims.UserID, claims.Username)
			}()
		}
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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

	var action string
	if r.Header.Get("HX-Request") == "true" {
		r.ParseForm()
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

	var err error
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
		writeError(w, http.StatusBadRequest, err.Error())
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
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

	var action string
	if r.Header.Get("HX-Request") == "true" {
		r.ParseForm()
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

	var err error
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
