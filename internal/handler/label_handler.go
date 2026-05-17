package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

type createLabelRequest struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, viewerID) {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

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
	if !validLabelColor(color) {
		writeError(w, http.StatusBadRequest, "color must be a valid hex color (e.g. #abc or #aabbcc)")
		return
	}

	label, err := h.Services.Label.Create(r.Context(), owner, repoName, name, color, description)
	if err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	labelID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	if err := h.Services.Label.Delete(r.Context(), owner, repoName, labelID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	labelID, err := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	if err := h.Services.Label.AddToIssue(r.Context(), owner, repoName, number, labelID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	labelID, err := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	if err := h.Services.Label.RemoveFromIssue(r.Context(), owner, repoName, number, labelID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	labelID, err := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	if err := h.Services.Label.AddToPull(r.Context(), owner, repoName, number, labelID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.recordPullLabelEvent(r, owner, repoName, number, labelID, model.PullEventLabeled)

	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Label added")
		h.renderPullLabelFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemovePullLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	labelID, err := strconv.ParseInt(chi.URLParam(r, "labelID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	if err := h.Services.Label.RemoveFromPull(r.Context(), owner, repoName, number, labelID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.recordPullLabelEvent(r, owner, repoName, number, labelID, model.PullEventUnlabeled)

	if r.Header.Get("HX-Request") == "true" {
		toast(w, "success", "Label removed")
		h.renderPullLabelFragment(w, r, owner, repoName, number)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// recordPullLabelEvent appends a labeled/unlabeled entry to the PR timeline.
// Best-effort: a lookup failure skips the event rather than failing the request.
func (h *Handler) recordPullLabelEvent(r *http.Request, owner, repoName string, number int, labelID int64, eventType string) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return
	}
	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		return
	}
	name := ""
	if labels, lerr := h.Services.Label.ListByRepo(r.Context(), owner, repoName); lerr == nil {
		for _, l := range labels {
			if l.ID == labelID {
				name = l.Name
				break
			}
		}
	}
	h.Services.PullEvent.Record(r.Context(), pull.ID, claims.UserID, claims.Username, eventType, name)
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

var hexColorRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func validLabelColor(color string) bool {
	return hexColorRe.MatchString(color)
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
