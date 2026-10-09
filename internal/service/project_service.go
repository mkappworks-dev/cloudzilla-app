package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrProjectNotFound = errors.New("project not found")
var ErrForbidden = errors.New("forbidden")

// ErrCardTargetNotFound covers an issue or pull request that is missing or lives
// in another repository; the two read alike so ids elsewhere can't be probed.
var ErrCardTargetNotFound = errors.New("issue or pull request not found in this repository")

var (
	ErrInvalidCard     = errors.New("invalid card")
	ErrInvalidAssignee = errors.New("assignee must be the owner or a collaborator")
	ErrInvalidLabel    = errors.New("label does not belong to this repository")
)

// ErrInvalidPosition re-exports the store sentinel so handlers map it to 400.
var ErrInvalidPosition = store.ErrInvalidPosition

// ProjectService manages Kanban project boards, columns, and cards.
type ProjectService struct {
	projects *store.ProjectStore
	repos    *RepoService
}

// NewProjectService creates a ProjectService backed by the given store and repo service.
func NewProjectService(projects *store.ProjectStore, repos *RepoService) *ProjectService {
	return &ProjectService{projects: projects, repos: repos}
}

// ProjectListView is a project board plus card stats for the list page.
type ProjectListView struct {
	model.Project
	CardCount   int
	LinkedCount int
	DoneCount   int
}

func (v ProjectListView) IsClosed() bool { return v.ClosedAt != nil }

// Progress is the percentage of linked cards that are done. It is 0 when the
// board has no issue/PR-linked cards, in which case the bar is hidden.
func (v ProjectListView) Progress() int {
	if v.LinkedCount == 0 {
		return 0
	}
	return v.DoneCount * 100 / v.LinkedCount
}

// ProjectList is a repo's project boards plus the open/closed tab counts.
type ProjectList struct {
	Projects    []ProjectListView
	OpenCount   int
	ClosedCount int
}

// ListByRepoWithStats returns a repo's project boards filtered by status
// ("open"/"closed"/"" for all) and a case-insensitive name query, with card
// stats per board and the open/closed tab counts.
func (s *ProjectService) ListByRepoWithStats(ctx context.Context, owner, repoName, status, query string) (*ProjectList, error) {
	repo, err := s.repos.Get(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	rows, err := s.projects.ListByRepoWithStats(ctx, repo.ID, query, status)
	if err != nil {
		return nil, err
	}
	open, closed, err := s.projects.CountByStatus(ctx, repo.ID, query)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectListView, len(rows))
	for i, r := range rows {
		out[i] = ProjectListView{
			Project:     r.Project,
			CardCount:   r.CardCount,
			LinkedCount: r.LinkedCount,
			DoneCount:   r.DoneCount,
		}
	}
	return &ProjectList{Projects: out, OpenCount: open, ClosedCount: closed}, nil
}

// SetProjectClosed closes or reopens a board. Requires write access.
func (s *ProjectService) SetProjectClosed(ctx context.Context, projectID, userID int64, closed bool) (*model.Project, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, ErrForbidden
	}
	if err := s.projects.SetProjectClosed(ctx, projectID, closed); err != nil {
		return nil, err
	}
	return s.GetProject(ctx, projectID)
}

func (s *ProjectService) GetProject(ctx context.Context, id int64) (*model.Project, error) {
	p, err := s.projects.GetProject(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project %d: %w", id, err)
	}
	return p, nil
}

func (s *ProjectService) repoForProject(ctx context.Context, projectID int64) (*model.Repository, error) {
	p, err := s.projects.GetProject(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project %d: %w", projectID, err)
	}
	repo, err := s.repos.GetByID(ctx, p.RepoID)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func (s *ProjectService) CreateProject(ctx context.Context, owner, repoName string, userID int64, name, description string) (*model.Project, error) {
	repo, err := s.repos.Get(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, ErrForbidden
	}
	p := &model.Project{RepoID: repo.ID, Name: name, Description: description}
	if err := s.projects.CreateProject(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ProjectService) DeleteProject(ctx context.Context, projectID, userID int64) error {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.repos.CanManage(ctx, repo, userID) {
		return ErrForbidden
	}
	return s.projects.DeleteProject(ctx, projectID)
}

func (s *ProjectService) CreateColumn(ctx context.Context, projectID, userID int64, name string) (*model.ProjectColumn, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, ErrForbidden
	}
	col := &model.ProjectColumn{ProjectID: projectID, Name: name}
	if err := s.projects.CreateColumn(ctx, col); err != nil {
		return nil, err
	}
	return col, nil
}

func (s *ProjectService) DeleteColumn(ctx context.Context, projectID, columnID, userID int64) error {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return ErrForbidden
	}
	return s.projects.DeleteColumn(ctx, columnID, projectID)
}

func (s *ProjectService) CreateCard(ctx context.Context, projectID, columnID, userID int64, d model.CardDetails) (*model.ProjectCard, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, ErrForbidden
	}
	colProject, err := s.projects.GetProjectByColumnID(ctx, columnID)
	if err != nil || colProject.ID != projectID {
		return nil, ErrProjectNotFound
	}
	if err := s.validateDetails(ctx, repo, d, true); err != nil {
		return nil, err
	}
	card := &model.ProjectCard{
		ColumnID: columnID,
		IssueID:  d.IssueID,
		PullID:   d.PullID,
		Title:    d.Title,
		Note:     d.Description,
		DueDate:  d.DueDate,
	}
	if err := s.projects.CreateCardWithPeople(ctx, card, d.AssigneeIDs, d.LabelIDs); err != nil {
		return nil, err
	}
	if err := s.projects.TouchProject(ctx, projectID); err != nil {
		log.Printf("TouchProject(%d): %v", projectID, err)
	}
	return card, nil
}

// validateDetails checks title, link, assignees and labels against the repo. A card without a
// title is only valid as a bare link, and only where linkOnlyOK says so.
func (s *ProjectService) validateDetails(ctx context.Context, repo *model.Repository, d model.CardDetails, linkOnlyOK bool) error {
	if d.IssueID != nil && d.PullID != nil {
		return ErrInvalidCard
	}
	linked := d.IssueID != nil || d.PullID != nil
	if strings.TrimSpace(d.Title) == "" && !(linked && linkOnlyOK) {
		return ErrInvalidCard
	}
	if len([]rune(d.Title)) > MaxTitleLen {
		return ErrInvalidCard
	}
	inRepo, err := s.projects.CardTargetsInRepo(ctx, repo.ID, d.IssueID, d.PullID)
	if err != nil {
		return err
	}
	if !inRepo {
		return ErrCardTargetNotFound
	}
	if len(d.AssigneeIDs) > 0 {
		allowed := map[int64]bool{repo.OwnerID: true}
		perms, err := s.repos.ListCollaborators(ctx, repo.ID)
		if err != nil {
			return err
		}
		for _, p := range perms {
			allowed[p.UserID] = true
		}
		for _, id := range d.AssigneeIDs {
			if !allowed[id] {
				return ErrInvalidAssignee
			}
		}
	}
	if len(d.LabelIDs) > 0 {
		ok, err := s.projects.LabelsInRepo(ctx, repo.ID, d.LabelIDs)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidLabel
		}
	}
	return nil
}

func (s *ProjectService) MoveCard(ctx context.Context, projectID, cardID, newColumnID int64, newPosition int, userID int64) error {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return ErrForbidden
	}
	colProject, err := s.projects.GetProjectByColumnID(ctx, newColumnID)
	if err != nil || colProject.ID != projectID {
		return ErrProjectNotFound
	}
	if newPosition < 0 {
		return fmt.Errorf("%w: %d", ErrInvalidPosition, newPosition)
	}
	if err := s.projects.MoveCard(ctx, projectID, cardID, newColumnID, newPosition); err != nil {
		if errors.Is(err, store.ErrCardNotInProject) {
			return ErrProjectNotFound
		}
		return err
	}
	if err := s.projects.TouchProject(ctx, projectID); err != nil {
		log.Printf("TouchProject(%d): %v", projectID, err)
	}
	return nil
}

func (s *ProjectService) DeleteCard(ctx context.Context, projectID, cardID, userID int64) error {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return ErrForbidden
	}
	return s.projects.DeleteCard(ctx, cardID, projectID)
}

func (s *ProjectService) UpdateCardDetails(ctx context.Context, projectID, cardID, userID int64, d model.CardDetails) error {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return ErrForbidden
	}
	card, err := s.projects.GetCardInProject(ctx, cardID, projectID)
	if errors.Is(err, store.ErrCardNotInProject) {
		return ErrProjectNotFound
	}
	if err != nil {
		return err
	}
	// A plain linked card (no stored title) may stay title-less; a titled card may not lose its title.
	if err := s.validateDetails(ctx, repo, d, card.Title == ""); err != nil {
		return err
	}
	if err := s.projects.SetCardDetails(ctx, cardID, projectID, d); err != nil {
		if errors.Is(err, store.ErrCardNotInProject) {
			return ErrProjectNotFound
		}
		return err
	}
	if err := s.projects.TouchProject(ctx, projectID); err != nil {
		log.Printf("TouchProject(%d): %v", projectID, err)
	}
	return nil
}

const cardTargetLimit = 8

// SearchCardTargets backs the board's "#" picker with the project repo's
// issues and pull requests matching a title fragment or a number.
func (s *ProjectService) SearchCardTargets(ctx context.Context, projectID, userID int64, query string) ([]model.CardTarget, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, ErrForbidden
	}
	query = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "#"))
	number, _ := strconv.Atoi(query)
	// number is compared against an INTEGER column; anything outside int4 can match nothing.
	if number < 0 || number > math.MaxInt32 {
		number = 0
	}
	return s.projects.SearchCardTargets(ctx, repo.ID, query, number, cardTargetLimit)
}

// ListColumnsWithCards returns all columns for a project, each with its cards pre-loaded.
func (s *ProjectService) ListColumnsWithCards(ctx context.Context, projectID int64) ([]ColumnWithCards, error) {
	cols, err := s.projects.ListColumns(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]ColumnWithCards, len(cols))
	for i, col := range cols {
		cards, err := s.projects.ListCardsByColumn(ctx, col.ID)
		if err != nil {
			return nil, err
		}
		if cards == nil {
			cards = []model.ProjectCard{}
		}
		result[i] = ColumnWithCards{Column: col, Cards: cards}
	}
	return result, nil
}

// ColumnWithCards pairs a ProjectColumn with its loaded cards.
type ColumnWithCards struct {
	Column model.ProjectColumn
	Cards  []model.ProjectCard
}

// KanbanCardView is the unified shape consumed by the project_detail kanban board.
// Title is the card's own title when it has one, else the linked issue or PR title.
// Number and State describe the linked item for issue and pull cards (0 and "" otherwise);
// LinkKind, LinkNumber and LinkState describe the link even when a custom title makes Kind "note".
type KanbanCardView struct {
	ID           int64
	Title        string
	Number       int
	State        string // "open" | "closed" | "merged" | "" (note)
	Kind         string // "issue" | "pull" | "note"
	Description  string
	DueDate      string // 2006-01-02, empty when unset
	Overdue      bool
	Assignees    []model.CardUser
	Labels       []model.Label
	LinkKind     string // "issue" | "pull" | "" (unlinked)
	LinkNumber   int
	LinkState    string
	RepoFullName string
	Position     int
	ColumnID     int64
}

// cardOverdue: a card whose linked item is already resolved is never overdue.
func cardOverdue(due *time.Time, linkState string, today time.Time) bool {
	if due == nil || linkState == "closed" || linkState == "merged" {
		return false
	}
	return due.Format("2006-01-02") < today.Format("2006-01-02")
}

// KanbanColumnView groups KanbanCardView entries under their owning column.
type KanbanColumnView struct {
	ID    int64
	Name  string
	Cards []KanbanCardView
}

// ListColumnsWithCardsExpanded resolves columns + cards joined with the owning
// repo's full name (owner/name) for display in the kanban board.
func (s *ProjectService) ListColumnsWithCardsExpanded(ctx context.Context, projectID int64) ([]KanbanColumnView, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	fullName := repo.OwnerName + "/" + repo.Name

	cols, err := s.ListColumnsWithCards(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var cardIDs []int64
	for _, col := range cols {
		for _, c := range col.Cards {
			cardIDs = append(cardIDs, c.ID)
		}
	}
	assignees, err := s.projects.CardAssignees(ctx, cardIDs)
	if err != nil {
		return nil, err
	}
	labels, err := s.projects.CardLabels(ctx, cardIDs)
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC()

	out := make([]KanbanColumnView, len(cols))
	for i, col := range cols {
		cards := make([]KanbanCardView, len(col.Cards))
		for j, c := range col.Cards {
			cv := KanbanCardView{
				ID:           c.ID,
				ColumnID:     c.ColumnID,
				Position:     c.Position,
				RepoFullName: fullName,
				Description:  c.Note,
				Assignees:    assignees[c.ID],
				Labels:       labels[c.ID],
			}
			if c.DueDate != nil {
				cv.DueDate = c.DueDate.Format("2006-01-02")
			}
			switch {
			case c.IssueID != nil:
				cv.LinkKind, cv.LinkNumber, cv.LinkState = "issue", c.IssueNumber, c.IssueState
				cv.Kind, cv.Title, cv.Number, cv.State = "issue", c.IssueTitle, c.IssueNumber, c.IssueState
			case c.PullID != nil:
				cv.LinkKind, cv.LinkNumber, cv.LinkState = "pull", c.PullNumber, c.PullState
				cv.Kind, cv.Title, cv.Number, cv.State = "pull", c.PullTitle, c.PullNumber, c.PullState
			default:
				cv.Kind = "note"
			}
			if c.Title != "" {
				cv.Kind, cv.Title, cv.Number, cv.State = "note", c.Title, 0, ""
			}
			cv.Overdue = cardOverdue(c.DueDate, cv.LinkState, today)
			cards[j] = cv
		}
		out[i] = KanbanColumnView{ID: col.Column.ID, Name: col.Column.Name, Cards: cards}
	}
	return out, nil
}
