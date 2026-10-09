package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// milestoneSetFailureMessage maps SetIssue/SetPull sentinel errors to a
// user-safe toast. Returns "" when err is not a known sentinel; callers should
// log the raw error and emit a generic 500 in that case.
func milestoneSetFailureMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrMilestoneRepoMismatch):
		return "That milestone belongs to a different repository."
	case errors.Is(err, service.ErrMilestoneNotFound):
		return "That milestone no longer exists."
	}
	return ""
}

func writeMilestoneUpdateError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}
	slog.Error("operation failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func (h *Handler) ListMilestones(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	milestones, err := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName, viewerOf(r))
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
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	m, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number, viewerOf(r))
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

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
		return
	}

	var title, description string
	var dueDate *time.Time
	if r.Header.Get("HX-Request") == "true" || isFormEncoded(r) {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		title = r.FormValue("title")
		description = r.FormValue("description")
		if d := r.FormValue("due_date"); d != "" {
			t, perr := time.Parse("2006-01-02", d)
			if perr != nil {
				writeError(w, http.StatusBadRequest, "due date must be a valid date (YYYY-MM-DD)")
				return
			}
			dueDate = &t
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
			t, perr := time.Parse(time.RFC3339, *req.DueDate)
			if perr != nil {
				writeError(w, http.StatusBadRequest, "due_date must be RFC3339")
				return
			}
			dueDate = &t
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

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
		return
	}

	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}

	var req updateMilestoneRequest
	if r.Header.Get("HX-Request") == "true" || isFormEncoded(r) {
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
		m, err := h.Services.Milestone.Close(r.Context(), owner, repoName, number, &claims.UserID)
		if err != nil {
			writeMilestoneUpdateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, m)
		return
	}
	if req.State == "open" {
		m, err := h.Services.Milestone.Reopen(r.Context(), owner, repoName, number, &claims.UserID)
		if err != nil {
			writeMilestoneUpdateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, m)
		return
	}

	var dueDate *time.Time
	if req.DueDate != nil && *req.DueDate != "" {
		t, perr := time.Parse("2006-01-02", *req.DueDate)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "due_date must be YYYY-MM-DD")
			return
		}
		dueDate = &t
	}
	title := req.Title
	if title == "" {
		existing, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number, &claims.UserID)
		if err != nil {
			writeError(w, http.StatusNotFound, "milestone not found")
			return
		}
		title = existing.Title
	}
	m, err := h.Services.Milestone.Update(r.Context(), owner, repoName, number, title, req.Description, dueDate, &claims.UserID)
	if err != nil {
		writeMilestoneUpdateError(w, err)
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

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
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

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
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
		// A malformed milestone_id used to silently coerce to nil — the
		// "clear milestone" sentinel — so a "set" with a typo would land as
		// "clear" with a success toast. Refuse with 400 instead.
		id, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid milestone id")
			return
		}
		milestoneID = &id
	}

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, issueNumber, &claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err := h.Services.Milestone.SetIssue(r.Context(), issue.ID, milestoneID); err != nil {
		if msg := milestoneSetFailureMessage(err); msg != "" {
			toast(w, "error", msg)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		slog.Error("set issue milestone failed", "owner", owner, "repo", repoName, "issue", issueNumber, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	allMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName, &claims.UserID)
	var currentMilestone *model.Milestone
	if milestoneID != nil {
		currentMilestone, _ = h.Services.Milestone.GetByID(r.Context(), *milestoneID, &claims.UserID)
	}
	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	canWrite := false
	if repo != nil {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}
	if milestoneID != nil {
		toast(w, "success", "Milestone set")
	} else {
		toast(w, "success", "Milestone cleared")
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

	if _, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID); !ok {
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
		id, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid milestone id")
			return
		}
		milestoneID = &id
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, pullNumber)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if err := h.Services.Milestone.SetPull(r.Context(), pull.ID, milestoneID); err != nil {
		if msg := milestoneSetFailureMessage(err); msg != "" {
			toast(w, "error", msg)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		slog.Error("set pull milestone failed", "owner", owner, "repo", repoName, "pull", pullNumber, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	allMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName, &claims.UserID)
	var currentMilestone *model.Milestone
	if milestoneID != nil {
		currentMilestone, _ = h.Services.Milestone.GetByID(r.Context(), *milestoneID, &claims.UserID)
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
