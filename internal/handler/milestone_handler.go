package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
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
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
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
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "milestones", canManage),
		Repo:             *repo,
		Owner:            owner,
		RepoName:         repoName,
		OpenMilestones:   open,
		ClosedMilestones: closed,
		CanWrite:         canWrite,
	}))
}

// PageNewMilestone renders the dedicated milestone creation page.
func (h *Handler) PageNewMilestone(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)

	h.render(w, r, pages.MilestoneNew(view.MilestoneNewData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "milestones", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		CanWrite: true,
	}))
}

// PageNewMilestoneSubmit handles the new-milestone form submission.
func (h *Handler) PageNewMilestoneSubmit(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	description := r.FormValue("description")
	dueRaw := r.FormValue("due_date")

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	renderErr := func(msg string) {
		h.render(w, r, pages.MilestoneNew(view.MilestoneNewData{
			BasePage:    h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "milestones", canManage),
			Repo:        *repo,
			Owner:       owner,
			RepoName:    repoName,
			CanWrite:    true,
			Error:       msg,
			Title:       title,
			Description: description,
			DueDate:     dueRaw,
		}))
	}

	if title == "" {
		renderErr("Title is required")
		return
	}

	var dueDate *time.Time
	if dueRaw != "" {
		t, err := time.Parse("2006-01-02", dueRaw)
		if err != nil {
			renderErr("Due date must be a valid date")
			return
		}
		dueDate = &t
	}

	if _, err := h.Services.Milestone.Create(r.Context(), owner, repoName, title, description, dueDate); err != nil {
		slog.Error("create milestone: service create failed",
			"owner", owner, "repo", repoName, "title", title, "error", err)
		renderErr("Failed to create milestone: " + err.Error())
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/%s/%s/milestones", owner, repoName), http.StatusSeeOther)
}

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

const milestoneItemsPerPage = 10

func (h *Handler) PageMilestoneDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		h.NotFound(w, r)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	var userID *int64
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	m, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	h.renderMilestoneDetail(w, r, repo, m, canWrite)
}

func (h *Handler) renderMilestoneDetail(w http.ResponseWriter, r *http.Request, repo *model.Repository, m *model.Milestone, canWrite bool) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	tab := r.URL.Query().Get("tab")
	if tab != "pulls" {
		tab = "issues"
	}
	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	pullOpen, pullClosed, err := h.Services.Milestone.PullCounts(r.Context(), m.ID)
	if err != nil {
		slog.Warn("milestone detail: pull counts failed", "milestone", m.ID, "error", err)
	}

	total := m.OpenCount
	switch {
	case tab == "issues" && state == "closed":
		total = m.ClosedCount
	case tab == "pulls" && state == "open":
		total = pullOpen
	case tab == "pulls" && state == "closed":
		total = pullClosed
	}
	totalPages := (total + milestoneItemsPerPage - 1) / milestoneItemsPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var issues []model.Issue
	var pulls []model.PullRequest
	if tab == "pulls" {
		if pulls, err = h.Services.Milestone.ListPulls(r.Context(), m.ID, state, page, milestoneItemsPerPage); err != nil {
			slog.Warn("milestone detail: pulls list failed", "milestone", m.ID, "error", err)
		}
	} else {
		if issues, err = h.Services.Milestone.ListIssues(r.Context(), m.ID, state, page, milestoneItemsPerPage); err != nil {
			slog.Warn("milestone detail: issues list failed", "milestone", m.ID, "error", err)
		}
	}

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	h.render(w, r, pages.MilestoneDetail(view.MilestoneDetailData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "milestones", canManage),
		Repo:             *repo,
		Owner:            owner,
		RepoName:         repoName,
		Milestone:        *m,
		CanWrite:         canWrite,
		Tab:              tab,
		State:            state,
		Page:             page,
		Issues:           issues,
		Pulls:            pulls,
		IssueOpenCount:   m.OpenCount,
		IssueClosedCount: m.ClosedCount,
		PullOpenCount:    pullOpen,
		PullClosedCount:  pullClosed,
		TotalCount:       total,
		TotalPages:       totalPages,
		PerPage:          milestoneItemsPerPage,
		DescriptionHTML:  renderMentionsHTML(markdown.Render(m.Description)),
	}))
}

func (h *Handler) PageMilestoneDetailAction(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}

	detailURL := fmt.Sprintf("/%s/%s/milestones/%d", owner, repoName, number)

	switch r.FormValue("action") {
	case "close":
		if _, err := h.Services.Milestone.Close(r.Context(), owner, repoName, number); err != nil {
			slog.Error("milestone detail: close failed", "owner", owner, "repo", repoName, "number", number, "error", err)
			http.Error(w, "failed to close milestone", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, detailURL, http.StatusSeeOther)

	case "reopen":
		if _, err := h.Services.Milestone.Reopen(r.Context(), owner, repoName, number); err != nil {
			slog.Error("milestone detail: reopen failed", "owner", owner, "repo", repoName, "number", number, "error", err)
			http.Error(w, "failed to reopen milestone", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, detailURL, http.StatusSeeOther)

	case "delete":
		if err := h.Services.Milestone.Delete(r.Context(), owner, repoName, number); err != nil {
			slog.Error("milestone detail: delete failed", "owner", owner, "repo", repoName, "number", number, "error", err)
			http.Error(w, "failed to delete milestone", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/%s/milestones", owner, repoName), http.StatusSeeOther)

	default:
		http.Redirect(w, r, detailURL, http.StatusSeeOther)
	}
}

func (h *Handler) milestoneFragmentContext(w http.ResponseWriter, r *http.Request, owner, repoName string, number int) (*model.Milestone, bool, bool) {
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return nil, false, false
	}
	var userID *int64
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		writeError(w, http.StatusNotFound, "milestone not found")
		return nil, false, false
	}
	m, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "milestone not found")
		return nil, false, false
	}
	return m, canWrite, true
}

func (h *Handler) milestoneWriteContext(w http.ResponseWriter, r *http.Request, owner, repoName string, number int) (*model.Milestone, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return nil, false
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	m, err := h.Services.Milestone.GetByNumber(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "milestone not found")
		return nil, false
	}
	return m, true
}

func (h *Handler) MilestoneTitleSection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
	m, canWrite, ok := h.milestoneFragmentContext(w, r, owner, repoName, number)
	if !ok {
		return
	}
	h.render(w, r, fragments.MilestoneTitleSection(owner, repoName, number, m.Title, canWrite, r.URL.Query().Get("mode") == "edit"))
}

func (h *Handler) EditMilestoneTitle(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
	m, ok := h.milestoneWriteContext(w, r, owner, repoName, number)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	updated, err := h.Services.Milestone.Update(r.Context(), owner, repoName, number, title, m.Description, m.DueDate)
	if err != nil {
		slog.Error("edit milestone title failed", "owner", owner, "repo", repoName, "number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.render(w, r, fragments.MilestoneTitleSection(owner, repoName, number, updated.Title, true, false))
}

func (h *Handler) MilestoneBodySection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
	m, canWrite, ok := h.milestoneFragmentContext(w, r, owner, repoName, number)
	if !ok {
		return
	}
	h.render(w, r, fragments.MilestoneBodyCard(view.MilestoneBodyCardData{
		Owner:           owner,
		RepoName:        repoName,
		Number:          number,
		Description:     m.Description,
		DescriptionHTML: renderMentionsHTML(markdown.Render(m.Description)),
		CanWrite:        canWrite,
		Editing:         r.URL.Query().Get("mode") == "edit",
	}))
}

func (h *Handler) EditMilestoneBody(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
	m, ok := h.milestoneWriteContext(w, r, owner, repoName, number)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	description := r.FormValue("description")
	updated, err := h.Services.Milestone.Update(r.Context(), owner, repoName, number, m.Title, description, m.DueDate)
	if err != nil {
		slog.Error("edit milestone body failed", "owner", owner, "repo", repoName, "number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.render(w, r, fragments.MilestoneBodyCard(view.MilestoneBodyCardData{
		Owner:           owner,
		RepoName:        repoName,
		Number:          number,
		Description:     updated.Description,
		DescriptionHTML: renderMentionsHTML(markdown.Render(updated.Description)),
		CanWrite:        true,
		Editing:         false,
	}))
}

func (h *Handler) MilestoneDueSection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
	m, canWrite, ok := h.milestoneFragmentContext(w, r, owner, repoName, number)
	if !ok {
		return
	}
	h.render(w, r, fragments.MilestoneDueSection(owner, repoName, number, m.DueDate, canWrite, r.URL.Query().Get("mode") == "edit"))
}

func (h *Handler) EditMilestoneDue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone number")
		return
	}
	m, ok := h.milestoneWriteContext(w, r, owner, repoName, number)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var dueDate *time.Time
	if raw := strings.TrimSpace(r.FormValue("due_date")); raw != "" {
		t, perr := time.Parse("2006-01-02", raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "due date must be a valid date")
			return
		}
		dueDate = &t
	}
	updated, err := h.Services.Milestone.Update(r.Context(), owner, repoName, number, m.Title, m.Description, dueDate)
	if err != nil {
		slog.Error("edit milestone due date failed", "owner", owner, "repo", repoName, "number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.render(w, r, fragments.MilestoneDueSection(owner, repoName, number, updated.DueDate, true, false))
}
