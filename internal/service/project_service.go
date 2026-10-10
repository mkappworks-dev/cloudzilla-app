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

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrProjectNotFound = errors.New("project not found")
var ErrForbidden = errors.New("forbidden")

// ErrCardTargetNotFound covers an issue or pull request that is missing or lives
// in another repository; the two read alike so ids elsewhere can't be probed.
var ErrCardTargetNotFound = errors.New("issue or pull request not found in this repository")

var (
	ErrInvalidCard        = errors.New("invalid card")
	ErrInvalidAssignee    = errors.New("assignee must be the owner or a collaborator")
	ErrInvalidLabel       = errors.New("label does not belong to this repository")
	ErrDescriptionTooLong = fmt.Errorf("description must be at most %d bytes", MaxCardDescriptionBytes)
	ErrTooManyAssignees   = fmt.Errorf("a card takes at most %d assignees", MaxCardAssignees)
	ErrTooManyLabels      = fmt.Errorf("a card takes at most %d labels", MaxCardLabels)
)

const (
	MaxCardDescriptionBytes = 65536
	MaxCardAssignees        = 50
	MaxCardLabels           = 50
)

// ErrInvalidPosition re-exports the store sentinel so handlers map it to 400.
var ErrInvalidPosition = store.ErrInvalidPosition

// ProjectService manages Kanban project boards, columns, and cards.
type ProjectService struct {
	projects *store.ProjectStore
	repos    *RepoService

	issues        *IssueService
	issueStore    *store.IssueStore
	labelStore    *store.LabelStore
	assigneeStore *store.AssigneeStore
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
	if err := s.projects.DeleteColumn(ctx, columnID, projectID); err != nil {
		if errors.Is(err, store.ErrColumnNotInProject) {
			return ErrProjectNotFound
		}
		return err
	}
	return nil
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
	if err := s.validateDetails(ctx, repo, d, true, nil); err != nil {
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
// title is only valid as a bare link, and only where linkOnlyOK says so. An assignee in
// current may stay even after losing repo access, so the card modal can save around a stale one.
func (s *ProjectService) validateDetails(ctx context.Context, repo *model.Repository, d model.CardDetails, linkOnlyOK bool, current []model.CardUser) error {
	if err := checkCardShape(d, linkOnlyOK); err != nil {
		return err
	}
	if err := s.checkLink(ctx, repo, d.IssueID, d.PullID); err != nil {
		return err
	}
	if len(d.AssigneeIDs) > 0 {
		allowed, err := s.assigneeAllowList(ctx, repo)
		if err != nil {
			return err
		}
		if err := checkAssignees(d.AssigneeIDs, allowed, current); err != nil {
			return err
		}
	}
	return s.checkLabels(ctx, repo, d.LabelIDs)
}

// checkCardShape is the part of validateDetails that needs no other table.
func checkCardShape(d model.CardDetails, linkOnlyOK bool) error {
	if d.IssueID != nil && d.PullID != nil {
		return ErrInvalidCard
	}
	linked := d.IssueID != nil || d.PullID != nil
	if strings.TrimSpace(d.Title) == "" && (!linked || !linkOnlyOK) {
		return ErrInvalidCard
	}
	if len([]rune(d.Title)) > MaxTitleLen {
		return ErrInvalidCard
	}
	switch {
	case len(d.Description) > MaxCardDescriptionBytes:
		return ErrDescriptionTooLong
	case len(d.AssigneeIDs) > MaxCardAssignees:
		return ErrTooManyAssignees
	case len(d.LabelIDs) > MaxCardLabels:
		return ErrTooManyLabels
	}
	return nil
}

func (s *ProjectService) checkLink(ctx context.Context, repo *model.Repository, issueID, pullID *int64) error {
	inRepo, err := s.projects.CardTargetsInRepo(ctx, repo.ID, issueID, pullID)
	if err != nil {
		return err
	}
	if !inRepo {
		return ErrCardTargetNotFound
	}
	return nil
}

func (s *ProjectService) checkLabels(ctx context.Context, repo *model.Repository, labelIDs []int64) error {
	if len(labelIDs) == 0 {
		return nil
	}
	ok, err := s.projects.LabelsInRepo(ctx, repo.ID, labelIDs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidLabel
	}
	return nil
}

// assigneeAllowList is the repo owner and its collaborators.
func (s *ProjectService) assigneeAllowList(ctx context.Context, repo *model.Repository) (map[int64]bool, error) {
	allowed := map[int64]bool{repo.OwnerID: true}
	perms, err := s.repos.ListCollaborators(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range perms {
		allowed[p.UserID] = true
	}
	return allowed, nil
}

// checkAssignees allows the allow list plus whoever is already on the card.
func checkAssignees(ids []int64, allowed map[int64]bool, current []model.CardUser) error {
	onCard := map[int64]bool{}
	for _, u := range current {
		onCard[u.ID] = true
	}
	for _, id := range ids {
		if !allowed[id] && !onCard[id] {
			return ErrInvalidAssignee
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
	if err := s.projects.DeleteCard(ctx, cardID, projectID); err != nil {
		if errors.Is(err, store.ErrCardNotInProject) {
			return ErrProjectNotFound
		}
		return err
	}
	return nil
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
	assigned, err := s.projects.CardAssignees(ctx, []int64{cardID})
	if err != nil {
		return err
	}
	if err := s.validateDetails(ctx, repo, d, card.Title == "", assigned[cardID]); err != nil {
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

// CardFieldsView is a card after a partial update, with what the card modal redraws.
type CardFieldsView struct {
	model.ProjectCard
	// DescriptionHTML is the description rendered like the board's, with #N linked for the caller.
	DescriptionHTML string           `json:"description_html"`
	Assignees       []model.CardUser `json:"assignees"`
	Labels          []model.Label    `json:"labels"`
}

// UpdateCardFields applies a partial update. The merged card is checked by the same rules as
// UpdateCardDetails, against the state stored under the card's row lock.
func (s *ProjectService) UpdateCardFields(ctx context.Context, projectID, cardID, userID int64, p model.CardPatch) (*CardFieldsView, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, ErrForbidden
	}
	// Everything validation reads from other tables is read here, before the row lock: a save
	// that held the lock while waiting for a second pool connection could starve the pool.
	// Only the links and labels the patch sets are checked; stored ones passed when saved.
	var linkErr, labelErr error
	if p.IssueID.Set || p.PullID.Set {
		if linkErr = s.checkLink(ctx, repo, p.IssueID.Value, p.PullID.Value); linkErr != nil && !errors.Is(linkErr, ErrCardTargetNotFound) {
			return nil, linkErr
		}
	}
	if p.LabelIDs.Set {
		if labelErr = s.checkLabels(ctx, repo, p.LabelIDs.Value); labelErr != nil && !errors.Is(labelErr, ErrInvalidLabel) {
			return nil, labelErr
		}
	}
	var allowed map[int64]bool
	if p.AssigneeIDs.Set && len(p.AssigneeIDs.Value) > 0 {
		if allowed, err = s.assigneeAllowList(ctx, repo); err != nil {
			return nil, err
		}
	}
	err = s.projects.MergeCardDetails(ctx, cardID, projectID, func(cur model.CardDetails, assigned []model.CardUser) (model.CardDetails, error) {
		next := p.Apply(cur)
		// In validateDetails' order. As in UpdateCardDetails, only a card stored without a
		// title may stay title-less.
		if err := checkCardShape(next, cur.Title == ""); err != nil {
			return next, err
		}
		if linkErr != nil {
			return next, linkErr
		}
		if allowed != nil {
			if err := checkAssignees(next.AssigneeIDs, allowed, assigned); err != nil {
				return next, err
			}
		}
		return next, labelErr
	})
	if errors.Is(err, store.ErrCardNotInProject) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.projects.TouchProject(ctx, projectID); err != nil {
		log.Printf("TouchProject(%d): %v", projectID, err)
	}

	card, err := s.projects.GetCardInProject(ctx, cardID, projectID)
	if errors.Is(err, store.ErrCardNotInProject) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	assignees, err := s.projects.CardAssignees(ctx, []int64{cardID})
	if err != nil {
		return nil, err
	}
	labels, err := s.projects.CardLabels(ctx, []int64{cardID})
	if err != nil {
		return nil, err
	}
	view := &CardFieldsView{ProjectCard: *card, Assignees: []model.CardUser{}, Labels: []model.Label{}}
	view.Assignees = append(view.Assignees, assignees[cardID]...)
	view.Labels = append(view.Labels, labels[cardID]...)
	if card.Note != "" {
		kinds, err := s.projects.RefKinds(ctx, repo.ID, refNumbers([]string{card.Note}), &userID)
		if err != nil {
			return nil, err
		}
		view.DescriptionHTML = markdown.RenderWithRefs(ctx, card.Note, "/"+repo.OwnerName+"/"+repo.Name, kinds)
	}
	return view, nil
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
// A nil viewer is anonymous.
func (s *ProjectService) ListColumnsWithCards(ctx context.Context, projectID int64, viewerID *int64) ([]ColumnWithCards, error) {
	cols, err := s.projects.ListColumns(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]ColumnWithCards, len(cols))
	for i, col := range cols {
		cards, err := s.projects.ListCardsByColumn(ctx, col.ID, viewerID)
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
// Kind "hidden" is a plain card for a private issue the viewer can't see: every other field is empty.
type KanbanCardView struct {
	ID          int64
	Title       string
	Number      int
	State       string // "open" | "closed" | "merged" | "" (note)
	Kind        string // "issue" | "pull" | "note" | "hidden"
	Description string
	// DescriptionHTML is Description rendered with #N linked to the repo's issues and PRs.
	DescriptionHTML string
	DueDate         string // 2006-01-02, empty when unset
	Overdue         bool
	Assignees       []model.CardUser
	Labels          []model.Label
	LinkKind        string // "issue" | "pull" | "" (unlinked)
	LinkID          int64
	LinkNumber      int
	LinkState       string
	LinkTitle       string
	RepoFullName    string
	Position        int
	ColumnID        int64
}

// maxBoardRefs bounds the #N lookup so a huge description can't exceed the
// query's parameter limit; refs past it render as plain text.
const maxBoardRefs = 200

// refNumbers is the distinct #N references across notes, at most maxBoardRefs of them.
func refNumbers(notes []string) []int {
	var nums []int
	seen := map[int]bool{}
	for _, note := range notes {
		for _, n := range markdown.RefNumbers(note) {
			if seen[n] {
				continue
			}
			if len(nums) == maxBoardRefs {
				return nums
			}
			seen[n] = true
			nums = append(nums, n)
		}
	}
	return nums
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

// linkedPeople holds the labels and assignees of linked issues or pull requests, keyed by their id.
type linkedPeople struct {
	labels    map[int64][]model.Label
	assignees map[int64][]model.CardUser
}

// loadLinkedPeople costs one query per table however many cards share the board.
func (s *ProjectService) loadLinkedPeople(ctx context.Context, kind string, ids []int64) (linkedPeople, error) {
	labels, err := s.projects.LinkedLabels(ctx, kind, ids)
	if err != nil {
		return linkedPeople{}, err
	}
	assignees, err := s.projects.LinkedAssignees(ctx, kind, ids)
	if err != nil {
		return linkedPeople{}, err
	}
	return linkedPeople{labels: labels, assignees: assignees}, nil
}

// ListColumnsWithCardsExpanded resolves columns + cards joined with the owning
// repo's full name (owner/name) for display in the kanban board.
func (s *ProjectService) ListColumnsWithCardsExpanded(ctx context.Context, projectID int64, viewerID *int64) ([]KanbanColumnView, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	fullName := repo.OwnerName + "/" + repo.Name

	cols, err := s.ListColumnsWithCards(ctx, projectID, viewerID)
	if err != nil {
		return nil, err
	}
	var cardIDs, plainIssueIDs, plainPullIDs []int64
	for _, col := range cols {
		for _, c := range col.Cards {
			cardIDs = append(cardIDs, c.ID)
			if c.Title != "" || c.IssueHidden {
				continue
			}
			if c.IssueID != nil {
				plainIssueIDs = append(plainIssueIDs, *c.IssueID)
			} else if c.PullID != nil {
				plainPullIDs = append(plainPullIDs, *c.PullID)
			}
		}
	}
	issuePeople, err := s.loadLinkedPeople(ctx, "issue", plainIssueIDs)
	if err != nil {
		return nil, err
	}
	pullPeople, err := s.loadLinkedPeople(ctx, "pull", plainPullIDs)
	if err != nil {
		return nil, err
	}
	assignees, err := s.projects.CardAssignees(ctx, cardIDs)
	if err != nil {
		return nil, err
	}
	labels, err := s.projects.CardLabels(ctx, cardIDs)
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, col := range cols {
		for _, c := range col.Cards {
			notes = append(notes, c.Note)
		}
	}
	kinds, err := s.projects.RefKinds(ctx, repo.ID, refNumbers(notes), viewerID)
	if err != nil {
		return nil, err
	}
	repoBase := "/" + fullName
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
			}
			// A plain card stands for its issue, so its own fields may describe the hidden issue too.
			if c.IssueHidden && c.Title == "" {
				cv.Kind = "hidden"
				cards[j] = cv
				continue
			}
			cv.Description, cv.Assignees, cv.Labels = c.Note, assignees[c.ID], labels[c.ID]
			if c.Note != "" {
				cv.DescriptionHTML = markdown.RenderWithRefs(ctx, c.Note, repoBase, kinds)
			}
			if c.DueDate != nil {
				cv.DueDate = c.DueDate.Format("2006-01-02")
			}
			switch {
			case c.IssueID != nil && !c.IssueHidden:
				cv.LinkKind, cv.LinkNumber, cv.LinkState = "issue", c.IssueNumber, c.IssueState
				cv.LinkID, cv.LinkTitle = *c.IssueID, c.IssueTitle
				cv.Kind, cv.Title, cv.Number, cv.State = "issue", c.IssueTitle, c.IssueNumber, c.IssueState
			case c.PullID != nil:
				cv.LinkKind, cv.LinkNumber, cv.LinkState = "pull", c.PullNumber, c.PullState
				cv.LinkID, cv.LinkTitle = *c.PullID, c.PullTitle
				cv.Kind, cv.Title, cv.Number, cv.State = "pull", c.PullTitle, c.PullNumber, c.PullState
			default:
				cv.Kind = "note"
			}
			if c.Title != "" {
				cv.Kind, cv.Title, cv.Number, cv.State = "note", c.Title, 0, ""
			}
			if c.Title == "" {
				switch {
				case c.IssueID != nil:
					cv.Labels, cv.Assignees = issuePeople.labels[*c.IssueID], issuePeople.assignees[*c.IssueID]
				case c.PullID != nil:
					cv.Labels, cv.Assignees = pullPeople.labels[*c.PullID], pullPeople.assignees[*c.PullID]
				}
			}
			cv.Overdue = cardOverdue(c.DueDate, cv.LinkState, today)
			cards[j] = cv
		}
		out[i] = KanbanColumnView{ID: col.Column.ID, Name: col.Column.Name, Cards: cards}
	}
	return out, nil
}
