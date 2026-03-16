package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
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
	repo := chi.URLParam(r, "repo")

	var req createIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issue, err := h.Services.Issue.Create(r.Context(), owner, repo, claims.UserID, req.Title, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, issue)
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
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

	issue, err := h.Services.Issue.SetState(r.Context(), owner, repo, number, model.IssueState(state))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-issue-detail", IssueDetailFragData{
			Issue: *issue, Owner: owner, Repo: repo,
		})
		return
	}
	writeJSON(w, http.StatusOK, issue)
}
