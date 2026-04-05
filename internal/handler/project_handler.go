package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// --- Page handlers ---

func (h *Handler) PageProjects(w http.ResponseWriter, r *http.Request) {
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
		userID = &claims.UserID
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	projects, err := h.Services.Project.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "failed to load projects", http.StatusInternalServerError)
		return
	}
	if projects == nil {
		projects = []model.Project{}
	}

	h.render(w, r, pages.Projects(view.ProjectsData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Projects: projects,
		CanWrite: canWrite,
	}))
}

func (h *Handler) PageProjectDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
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

	project, err := h.Services.Project.GetProject(r.Context(), projectID)
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if project.RepoID != repo.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	columns, err := h.Services.Project.ListColumnsWithCards(r.Context(), project.ID)
	if err != nil {
		http.Error(w, "failed to load board", http.StatusInternalServerError)
		return
	}
	if columns == nil {
		columns = []service.ColumnWithCards{}
	}

	h.render(w, r, pages.ProjectDetail(view.ProjectDetailData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Project:  *project,
		Columns:  columns,
		CanWrite: canWrite,
	}))
}

// --- API handlers ---

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	project, err := h.Services.Project.CreateProject(r.Context(), owner, repoName, claims.UserID, req.Name, req.Description)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if err := h.Services.Project.DeleteProject(r.Context(), projectID, claims.UserID); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type createColumnRequest struct {
	Name string `json:"name"`
}

func (h *Handler) CreateColumn(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var req createColumnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	col, err := h.Services.Project.CreateColumn(r.Context(), projectID, claims.UserID, req.Name)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, col)
}

func (h *Handler) DeleteColumn(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	columnID, err := strconv.ParseInt(chi.URLParam(r, "colID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid column id")
		return
	}
	if err := h.Services.Project.DeleteColumn(r.Context(), projectID, columnID, claims.UserID); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateCard(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var req struct {
		ColumnID int64  `json:"column_id"`
		IssueID  *int64 `json:"issue_id,omitempty"`
		PullID   *int64 `json:"pull_id,omitempty"`
		Note     string `json:"note,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ColumnID == 0 {
		writeError(w, http.StatusBadRequest, "column_id is required")
		return
	}
	if req.IssueID == nil && req.PullID == nil && strings.TrimSpace(req.Note) == "" {
		writeError(w, http.StatusBadRequest, "one of issue_id, pull_id, or note is required")
		return
	}
	card, err := h.Services.Project.CreateCard(r.Context(), projectID, req.ColumnID, claims.UserID, req.IssueID, req.PullID, req.Note)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

func (h *Handler) MoveCard(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	cardID, err := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	var req struct {
		ColumnID int64 `json:"column_id"`
		Position int64 `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ColumnID == 0 {
		writeError(w, http.StatusBadRequest, "column_id is required")
		return
	}
	if err := h.Services.Project.MoveCard(r.Context(), projectID, cardID, req.ColumnID, claims.UserID); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	cardID, err := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := h.Services.Project.DeleteCard(r.Context(), projectID, cardID, claims.UserID); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeProjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	slog.Error("operation failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
