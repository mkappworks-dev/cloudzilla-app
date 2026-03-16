package service

import (
	"context"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type CommentService struct {
	comments *store.CommentStore
}

func NewCommentService(comments *store.CommentStore) *CommentService {
	return &CommentService{comments: comments}
}

func (s *CommentService) CreateForIssue(ctx context.Context, repoID, issueID, authorID int64, body string) (*model.Comment, error) {
	c := &model.Comment{
		RepoID:   repoID,
		IssueID:  &issueID,
		AuthorID: authorID,
		Body:     body,
	}
	if err := s.comments.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *CommentService) CreateForPull(ctx context.Context, repoID, pullID, authorID int64, body string) (*model.Comment, error) {
	c := &model.Comment{
		RepoID:   repoID,
		PullID:   &pullID,
		AuthorID: authorID,
		Body:     body,
	}
	if err := s.comments.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *CommentService) ListByIssue(ctx context.Context, issueID int64) ([]model.Comment, error) {
	return s.comments.ListByIssue(ctx, issueID)
}

func (s *CommentService) ListByPull(ctx context.Context, pullID int64) ([]model.Comment, error) {
	return s.comments.ListByPull(ctx, pullID)
}

func (s *CommentService) Delete(ctx context.Context, id int64) error {
	return s.comments.Delete(ctx, id)
}
