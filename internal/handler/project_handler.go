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
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// --- Page handlers ---

func (h *Handler) PageProjects(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowProjects {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	list, err := h.Services.Project.ListByRepoWithStats(r.Context(), owner, repoName, state, query)
	if err != nil {
		http.Error(w, "failed to load projects", http.StatusInternalServerError)
		return
	}

	h.render(w, r, pages.Projects(view.ProjectsData{
		BasePage:    h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "projects", canManage),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Items:       list.Projects,
		StateFilter: state,
		SearchQuery: query,
		OpenCount:   list.OpenCount,
		ClosedCount: list.ClosedCount,
		CanWrite:    canWrite,
		CanManage:   canManage,
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

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowProjects {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	canWrite := false
	canManage := false
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		viewerID = &claims.UserID
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

	columns, err := h.Services.Project.ListColumnsWithCardsExpanded(r.Context(), project.ID, viewerID)
	if err != nil {
		http.Error(w, "failed to load board", http.StatusInternalServerError)
		return
	}
	if columns == nil {
		columns = []service.KanbanColumnView{}
	}

	data := view.ProjectDetailData{
		BasePage:  h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "projects", canManage),
		Repo:      *repo,
		Owner:     owner,
		RepoName:  repoName,
		Project:   *project,
		Columns:   columns,
		CanWrite:  canWrite,
		CanManage: canManage,
	}
	if canWrite {
		h.loadCardOptions(r, &data, repo)
	}
	var names []string
	for _, p := range data.People {
		names = append(names, p.Username)
	}
	for _, col := range columns {
		for _, c := range col.Cards {
			for _, a := range c.Assignees {
				names = append(names, a.Username)
			}
		}
	}
	h.render(w, h.withAvatars(r, names...), pages.ProjectDetail(data))
}

// loadCardOptions fills the card modal's label and people lists. Best-effort: a failed
// query leaves that picker empty rather than failing the board.
func (h *Handler) loadCardOptions(r *http.Request, data *view.ProjectDetailData, repo *model.Repository) {
	ctx := r.Context()
	if labels, err := h.Services.Label.ListByRepo(ctx, data.Owner, data.RepoName); err == nil {
		data.Labels = labels
	} else {
		slog.Warn("project board: list labels failed", "repo_id", repo.ID, "error", err)
	}
	seen := map[int64]bool{}
	if repo.OwnerID != 0 {
		data.People = append(data.People, model.CardUser{ID: repo.OwnerID, Username: repo.OwnerName})
		seen[repo.OwnerID] = true
	}
	collabs, err := h.Services.Repo.ListCollaborators(ctx, repo.ID)
	if err != nil {
		slog.Warn("project board: list collaborators failed", "repo_id", repo.ID, "error", err)
	}
	for _, c := range collabs {
		if !seen[c.UserID] && c.Username != "" {
			seen[c.UserID] = true
			data.People = append(data.People, model.CardUser{ID: c.UserID, Username: c.Username})
		}
	}
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

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowProjects {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
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

// The project services authorize against the project's own repo, so a project
// from another repo must 404 here or their 403 would confirm that it exists.
func (h *Handler) projectIDInRepo(w http.ResponseWriter, r *http.Request) (int64, bool) {
	_, projectID, ok := h.projectInRepo(w, r)
	return projectID, ok
}

func (h *Handler) projectInRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, int64, bool) {
	repo, ok := h.readableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return nil, 0, false
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return nil, 0, false
	}
	project, err := h.Services.Project.GetProject(r.Context(), projectID)
	if err != nil || project.RepoID != repo.ID {
		writeError(w, http.StatusNotFound, service.ErrProjectNotFound.Error())
		return nil, 0, false
	}
	return repo, project.ID, true
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	if err := h.Services.Project.DeleteProject(r.Context(), projectID, claims.UserID); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateProjectRequest struct {
	Closed bool `json:"closed"`
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, err := h.Services.Project.SetProjectClosed(r.Context(), projectID, claims.UserID, req.Closed)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
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
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	var req createColumnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
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
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
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
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	var req struct {
		ColumnID int64 `json:"column_id"`
		cardDetailsBody
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ColumnID == 0 {
		writeError(w, http.StatusBadRequest, "column_id is required")
		return
	}
	d, err := req.details()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	card, err := h.Services.Project.CreateCard(r.Context(), projectID, req.ColumnID, claims.UserID, d)
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
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
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
	if err := h.Services.Project.MoveCard(r.Context(), projectID, cardID, req.ColumnID, int(req.Position), claims.UserID); err != nil {
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
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
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

func (h *Handler) UpdateCardDetails(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	cardID, err := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	var req cardDetailsBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	d, err := req.details()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Services.Project.UpdateCardDetails(r.Context(), projectID, cardID, claims.UserID, d); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateCardFields saves the fields named in the body and leaves the others as stored, so the
// card modal can save one field at a time.
func (h *Handler) UpdateCardFields(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	cardID, err := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil || raw == nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	patch, err := cardPatch(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	card, err := h.Services.Project.UpdateCardFields(r.Context(), projectID, cardID, claims.UserID, patch)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// cardPatch reads the fields of a partial card update; a key that is absent stays unset.
func cardPatch(raw map[string]json.RawMessage) (model.CardPatch, error) {
	var p model.CardPatch
	for key, v := range raw {
		isNull := string(v) == "null"
		var err error
		switch key {
		case "title":
			p.Title.Set = true
			err = decodeField(v, isNull, &p.Title.Value)
			p.Title.Value = strings.TrimSpace(p.Title.Value)
		case "description":
			p.Description.Set = true
			err = decodeField(v, isNull, &p.Description.Value)
		case "assignee_ids":
			p.AssigneeIDs.Set = true
			err = decodeField(v, isNull, &p.AssigneeIDs.Value)
		case "label_ids":
			p.LabelIDs.Set = true
			err = decodeField(v, isNull, &p.LabelIDs.Value)
		case "issue_id":
			p.IssueID.Set = true
			err = json.Unmarshal(v, &p.IssueID.Value)
		case "pull_id":
			p.PullID.Set = true
			err = json.Unmarshal(v, &p.PullID.Value)
		case "due_date":
			p.DueDate.Set = true
			var s *string
			if json.Unmarshal(v, &s) != nil {
				return p, errors.New("due_date must be YYYY-MM-DD")
			}
			if s != nil && *s != "" {
				if p.DueDate.Value, err = parseDueDate(*s); err != nil {
					return p, err
				}
			}
		default:
			return p, fmt.Errorf("unknown field %q", key)
		}
		if err != nil {
			return p, fmt.Errorf("invalid %s", key)
		}
	}
	if p.Empty() {
		return p, errors.New("no fields to update")
	}
	return p, nil
}

// decodeField rejects null for fields that have no "unset" value: a string or an id list.
func decodeField(v json.RawMessage, isNull bool, dst any) error {
	if isNull {
		return errors.New("null")
	}
	return json.Unmarshal(v, dst)
}

func (h *Handler) ConvertCard(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, projectID, ok := h.projectInRepo(w, r)
	if !ok {
		return
	}
	cardID, err := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	card, issue, err := h.Services.Project.ConvertCardToIssue(r.Context(), projectID, cardID, claims.UserID)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	h.announceIssueOpened(claims, repo, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), issue)
	writeJSON(w, http.StatusOK, card)
}

type cardDetailsBody struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DueDate     string  `json:"due_date"`
	AssigneeIDs []int64 `json:"assignee_ids"`
	LabelIDs    []int64 `json:"label_ids"`
	IssueID     *int64  `json:"issue_id"`
	PullID      *int64  `json:"pull_id"`
}

func (b cardDetailsBody) details() (model.CardDetails, error) {
	d := model.CardDetails{
		Title:       strings.TrimSpace(b.Title),
		Description: b.Description,
		IssueID:     b.IssueID,
		PullID:      b.PullID,
		AssigneeIDs: b.AssigneeIDs,
		LabelIDs:    b.LabelIDs,
	}
	if b.DueDate != "" {
		t, err := parseDueDate(b.DueDate)
		if err != nil {
			return d, err
		}
		d.DueDate = t
	}
	return d, nil
}

func parseDueDate(s string) (*time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	// time.Parse accepts year 0, which Postgres DATE has no value for.
	if err != nil || t.Year() < 1 || t.Year() > 9999 {
		return nil, errors.New("due_date must be YYYY-MM-DD")
	}
	return &t, nil
}

func (h *Handler) SearchCardTargets(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projectID, ok := h.projectIDInRepo(w, r)
	if !ok {
		return
	}
	targets, err := h.Services.Project.SearchCardTargets(r.Context(), projectID, claims.UserID, r.URL.Query().Get("q"))
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func writeProjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrProjectNotFound) || errors.Is(err, service.ErrCardTargetNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if errors.Is(err, service.ErrNotConvertible) || errors.Is(err, service.ErrTitleTooLong) ||
		errors.Is(err, service.ErrInvalidPosition) || errors.Is(err, service.ErrInvalidCard) ||
		errors.Is(err, service.ErrInvalidAssignee) || errors.Is(err, service.ErrInvalidLabel) ||
		errors.Is(err, service.ErrDescriptionTooLong) || errors.Is(err, service.ErrTooManyAssignees) || errors.Is(err, service.ErrTooManyLabels) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Error("operation failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
