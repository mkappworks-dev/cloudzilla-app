package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

type createLabelRequest struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	labels, err := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if labels == nil {
		labels = []model.Label{}
	}

	if r.Header.Get("HX-Request") == "true" {
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok && repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		var repoID int64
		if repo != nil {
			repoID = repo.ID
		}
		h.render(w, r, fragments.RepoLabels(view.RepoLabelsFragData{
			Owner: owner, RepoName: repoName, RepoID: repoID,
			Labels: labels, CanWrite: canWrite,
		}))
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (h *Handler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	var name, color, description string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		name = r.FormValue("name")
		color = r.FormValue("color")
		description = r.FormValue("description")
	} else {
		var req createLabelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		name, color, description = req.Name, req.Color, req.Description
	}
	if color == "" {
		color = "#e5e5e5"
	}

	label, err := h.Services.Label.Create(r.Context(), owner, repoName, name, color, description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		labels, _ := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
		if labels == nil {
			labels = []model.Label{}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		var repoID int64
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok && repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
			repoID = repo.ID
		}
		h.render(w, r, fragments.RepoLabels(view.RepoLabelsFragData{
			Owner: owner, RepoName: repoName, RepoID: repoID,
			Labels: labels, CanWrite: canWrite,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, label)
}

func (h *Handler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	labelID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	if err := h.Services.Label.Delete(r.Context(), owner, repoName, labelID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		labels, _ := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
		if labels == nil {
			labels = []model.Label{}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		var repoID int64
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok && repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
			repoID = repo.ID
		}
		h.render(w, r, fragments.RepoLabels(view.RepoLabelsFragData{
			Owner: owner, RepoName: repoName, RepoID: repoID,
			Labels: labels, CanWrite: canWrite,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddIssueLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	labelID, _ := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)

	if err := h.Services.Label.AddToIssue(r.Context(), owner, repoName, number, labelID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderIssueLabelFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveIssueLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	labelID, _ := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)

	if err := h.Services.Label.RemoveFromIssue(r.Context(), owner, repoName, number, labelID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderIssueLabelFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddPullLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	labelID, _ := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)

	if err := h.Services.Label.AddToPull(r.Context(), owner, repoName, number, labelID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderPullLabelFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemovePullLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	labelID, _ := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)

	if err := h.Services.Label.RemoveFromPull(r.Context(), owner, repoName, number, labelID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderPullLabelFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renderIssueLabelFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, issueNumber int) {
	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
	}
	issue, _ := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, callerID)
	var labels, allLabels []model.Label
	if issue != nil {
		labels, _ = h.Services.Label.GetForIssue(r.Context(), issue.ID)
	}
	allLabels, _ = h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if labels == nil {
		labels = []model.Label{}
	}
	if allLabels == nil {
		allLabels = []model.Label{}
	}
	canWrite := false
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
	}
	h.render(w, r, fragments.IssueLabels(view.IssueLabelSidebarData{
		Owner: owner, RepoName: repoName, IssueNumber: issueNumber,
		Labels: labels, AllLabels: allLabels, CanWrite: canWrite,
	}))
}

func (h *Handler) renderPullLabelFragment(w http.ResponseWriter, r *http.Request, owner, repoName string, pullNumber int) {
	pull, _ := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	var labels, allLabels []model.Label
	if pull != nil {
		labels, _ = h.Services.Label.GetForPull(r.Context(), pull.ID)
	}
	allLabels, _ = h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if labels == nil {
		labels = []model.Label{}
	}
	if allLabels == nil {
		allLabels = []model.Label{}
	}
	canWrite := false
	if repo, err := h.Services.Repo.Get(r.Context(), owner, repoName); err == nil {
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
	}
	h.render(w, r, fragments.PullLabels(view.PullLabelSidebarData{
		Owner: owner, RepoName: repoName, PullNumber: pullNumber,
		Labels: labels, AllLabels: allLabels, CanWrite: canWrite,
	}))
}
