package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// DiscussionService manages repository discussions, replies, and categories.
type DiscussionService struct {
	discussions *store.DiscussionStore
	repos       *store.RepoStore
}

// NewDiscussionService creates a DiscussionService backed by the given stores.
func NewDiscussionService(discussions *store.DiscussionStore, repos *store.RepoStore) *DiscussionService {
	return &DiscussionService{discussions: discussions, repos: repos}
}

func (s *DiscussionService) ListCategories(ctx context.Context) ([]model.DiscussionCategory, error) {
	return s.discussions.ListCategories(ctx)
}

func (s *DiscussionService) GetCategory(ctx context.Context, id int64) (*model.DiscussionCategory, error) {
	return s.discussions.GetCategory(ctx, id)
}

func (s *DiscussionService) CountByRepo(ctx context.Context, repoID int64) (int, error) {
	return s.discussions.CountByRepo(ctx, repoID)
}

func (s *DiscussionService) List(ctx context.Context, owner, repoName string, categoryID int64) ([]model.Discussion, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.discussions.List(ctx, repo.ID, categoryID)
}

func (s *DiscussionService) Get(ctx context.Context, owner, repoName string, number int) (*model.Discussion, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	d, err := s.discussions.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("discussion not found")
	}
	return d, nil
}

func (s *DiscussionService) Create(ctx context.Context, owner, repoName string, authorID int64, authorName string, categoryID int64, title, body string) (*model.Discussion, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	d := &model.Discussion{
		RepoID:     repo.ID,
		CategoryID: categoryID,
		Title:      title,
		Body:       body,
		AuthorID:   authorID,
		AuthorName: authorName,
	}
	if err := s.discussions.Create(ctx, d); err != nil {
		return nil, fmt.Errorf("create discussion: %w", err)
	}
	return d, nil
}

func (s *DiscussionService) CreateReply(ctx context.Context, discussionID, authorID int64, authorName, body string, parentID *int64) (*model.DiscussionReply, error) {
	if body == "" {
		return nil, fmt.Errorf("body is required")
	}
	r := &model.DiscussionReply{
		DiscussionID: discussionID,
		AuthorID:     authorID,
		AuthorName:   authorName,
		Body:         body,
	}
	if parentID != nil {
		r.ParentID = sql.NullInt64{Int64: *parentID, Valid: true}
	}
	if err := s.discussions.CreateReply(ctx, r); err != nil {
		return nil, fmt.Errorf("create reply: %w", err)
	}
	return r, nil
}

func (s *DiscussionService) ListReplies(ctx context.Context, discussionID int64) ([]model.DiscussionReply, error) {
	return s.discussions.ListReplies(ctx, discussionID)
}

func (s *DiscussionService) SetAnswer(ctx context.Context, discussionID int64, replyID *int64) error {
	return s.discussions.SetAnswer(ctx, discussionID, replyID)
}

func (s *DiscussionService) Lock(ctx context.Context, discussionID int64, locked bool) error {
	return s.discussions.LockDiscussion(ctx, discussionID, locked)
}

func (s *DiscussionService) UpdateContent(ctx context.Context, discussionID int64, title, body string) error {
	if title == "" {
		return fmt.Errorf("title is required")
	}
	return s.discussions.UpdateContent(ctx, discussionID, title, body)
}

func (s *DiscussionService) SetCategory(ctx context.Context, discussionID, categoryID int64) error {
	return s.discussions.SetCategory(ctx, discussionID, categoryID)
}

func (s *DiscussionService) DeleteReply(ctx context.Context, id, discussionID int64) error {
	return s.discussions.DeleteReply(ctx, id, discussionID)
}
