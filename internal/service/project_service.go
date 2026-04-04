package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

var ErrProjectNotFound = errors.New("project not found")
var ErrForbidden = errors.New("forbidden")

type ProjectService struct {
	projects *store.ProjectStore
	repos    *RepoService
}

func NewProjectService(projects *store.ProjectStore, repos *RepoService) *ProjectService {
	return &ProjectService{projects: projects, repos: repos}
}

func (s *ProjectService) ListByRepo(ctx context.Context, owner, repoName string) ([]model.Project, error) {
	repo, err := s.repos.Get(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.projects.ListByRepo(ctx, repo.ID)
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

func (s *ProjectService) CreateCard(ctx context.Context, projectID, columnID, userID int64, issueID, pullID *int64, note string) (*model.ProjectCard, error) {
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
	card := &model.ProjectCard{
		ColumnID: columnID,
		IssueID:  issueID,
		PullID:   pullID,
		Note:     note,
	}
	if err := s.projects.CreateCard(ctx, card); err != nil {
		return nil, err
	}
	if err := s.projects.TouchProject(ctx, projectID); err != nil {
		log.Printf("TouchProject(%d): %v", projectID, err)
	}
	return card, nil
}

func (s *ProjectService) MoveCard(ctx context.Context, projectID, cardID, newColumnID, userID int64) error {
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
	if err := s.projects.MoveCard(ctx, cardID, newColumnID); err != nil {
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
