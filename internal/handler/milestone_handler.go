package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageMilestones(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	all, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	var open, closed []model.Milestone
	for _, m := range all {
		if m.State == "open" {
			open = append(open, m)
		} else {
			closed = append(closed, m)
		}
	}

	h.render(w, r, pages.Milestones(view.MilestonesData{
		BasePage:         basePage(r, h.Services),
		Repo:             *repo,
		Owner:            owner,
		RepoName:         repoName,
		OpenMilestones:   open,
		ClosedMilestones: closed,
		CanWrite:         canWrite,
	}))
}

// ─── API handlers ────────────────────────────────────────────────────────────

func (h *Handler) ListMilestones(w http.ResponseWriter, r *http.Request) {
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
	milestones, err := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, milestones)
}

func (h *Handler) GetMilestone(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
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
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}
	m, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type createMilestoneRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DueDate     *string `json:"due_date"` // RFC3339 or empty
}

func (h *Handler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var title, description string
	var dueDate *time.Time
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		title = r.FormValue("title")
		description = r.FormValue("description")
		if d := r.FormValue("due_date"); d != "" {
			t, err := time.Parse("2006-01-02", d)
			if err == nil {
				dueDate = &t
			}
		}
	} else {
		var req createMilestoneRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		title = req.Title
		description = req.Description
		if req.DueDate != nil && *req.DueDate != "" {
			t, err := time.Parse(time.RFC3339, *req.DueDate)
			if err == nil {
				dueDate = &t
			}
		}
	}

	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	m, err := h.Services.Milestone.Create(r.Context(), owner, repoName, title, description, dueDate)
	if err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		all, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
		var open, closed []model.Milestone
		for _, ms := range all {
			if ms.State == "open" {
				open = append(open, ms)
			} else {
				closed = append(closed, ms)
			}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		if repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		h.render(w, r, fragments.MilestonesList(view.MilestonesListFragData{
			Owner:            owner,
			RepoName:         repoName,
			OpenMilestones:   open,
			ClosedMilestones: closed,
			CanWrite:         canWrite,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

type updateMilestoneRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	DueDate     *string `json:"due_date"`
}

func (h *Handler) UpdateMilestone(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

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
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}

	var req updateMilestoneRequest
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		req.Title = r.FormValue("title")
		req.Description = r.FormValue("description")
		req.State = r.FormValue("state")
		req.DueDate = func() *string { s := r.FormValue("due_date"); return &s }()
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Handle state transitions
	if req.State == "closed" {
		m, err := h.Services.Milestone.Close(r.Context(), owner, repoName, number)
		if err != nil {
			slog.Error("operation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, m)
		return
	}
	if req.State == "open" {
		m, err := h.Services.Milestone.Reopen(r.Context(), owner, repoName, number)
		if err != nil {
			slog.Error("operation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, m)
		return
	}

	// Field update
	var dueDate *time.Time
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse("2006-01-02", *req.DueDate)
		if err == nil {
			dueDate = &t
		}
	}
	title := req.Title
	if title == "" {
		existing, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number)
		if err != nil {
			writeError(w, http.StatusNotFound, "milestone not found")
			return
		}
		title = existing.Title
	}
	m, err := h.Services.Milestone.Update(r.Context(), owner, repoName, number, title, req.Description, dueDate)
	if err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *Handler) DeleteMilestone(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

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
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}

	if err := h.Services.Milestone.Delete(r.Context(), owner, repoName, number); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		all, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
		var open, closed []model.Milestone
		for _, ms := range all {
			if ms.State == "open" {
				open = append(open, ms)
			} else {
				closed = append(closed, ms)
			}
		}
		repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
		canWrite := false
		if repo != nil {
			canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		}
		h.render(w, r, fragments.MilestonesList(view.MilestonesListFragData{
			Owner:            owner,
			RepoName:         repoName,
			OpenMilestones:   open,
			ClosedMilestones: closed,
			CanWrite:         canWrite,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetIssueMilestone assigns/removes a milestone from an issue via sidebar.
func (h *Handler) SetIssueMilestone(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	issueNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var milestoneID *int64
	if s := r.FormValue("milestone_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			milestoneID = &id
		}
	}

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, &claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err := h.Services.Milestone.SetIssue(r.Context(), issue.ID, milestoneID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return updated sidebar fragment
	allMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	var currentMilestone *model.Milestone
	if milestoneID != nil {
		currentMilestone, _ = h.Services.Milestone.GetByID(r.Context(), *milestoneID)
	}
	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	canWrite := false
	if repo != nil {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}
	h.render(w, r, fragments.MilestoneSidebar(view.MilestoneSidebarFragData{
		Owner:         owner,
		RepoName:      repoName,
		ItemNumber:    issueNumber,
		IsPull:        false,
		Current:       currentMilestone,
		AllMilestones: allMilestones,
		CanWrite:      canWrite,
	}))
}

// SetPullMilestone assigns/removes a milestone from a pull request via sidebar.
func (h *Handler) SetPullMilestone(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	pullNumber, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var milestoneID *int64
	if s := r.FormValue("milestone_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			milestoneID = &id
		}
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if err := h.Services.Milestone.SetPull(r.Context(), pull.ID, milestoneID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return updated sidebar fragment
	allMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	var currentMilestone *model.Milestone
	if milestoneID != nil {
		currentMilestone, _ = h.Services.Milestone.GetByID(r.Context(), *milestoneID)
	}
	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	canWrite := false
	if repo != nil {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}
	h.render(w, r, fragments.MilestoneSidebar(view.MilestoneSidebarFragData{
		Owner:         owner,
		RepoName:      repoName,
		ItemNumber:    pullNumber,
		IsPull:        true,
		Current:       currentMilestone,
		AllMilestones: allMilestones,
		CanWrite:      canWrite,
	}))
}
