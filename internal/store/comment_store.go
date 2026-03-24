package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type CommentStore struct{ q *db.Queries }

func NewCommentStore(q *db.Queries) *CommentStore { return &CommentStore{q: q} }

func (s *CommentStore) Create(ctx context.Context, c *model.Comment) error {
	var issueID, pullID sql.NullInt64
	if c.IssueID != nil && *c.IssueID != 0 {
		issueID = sql.NullInt64{Int64: *c.IssueID, Valid: true}
	}
	if c.PullID != nil && *c.PullID != 0 {
		pullID = sql.NullInt64{Int64: *c.PullID, Valid: true}
	}

	result, err := s.q.CreateComment(ctx, db.CreateCommentParams{
		RepoID:     c.RepoID,
		IssueID:    issueID,
		PullID:     pullID,
		AuthorID:   c.AuthorID,
		AuthorName: c.AuthorName,
		Body:       c.Body,
	})
	if err != nil {
		return fmt.Errorf("comment create: %w", err)
	}
	c.ID = result.ID
	c.AuthorName = result.AuthorName
	c.CreatedAt = result.CreatedAt
	c.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *CommentStore) GetByID(ctx context.Context, id int64) (*model.Comment, error) {
	result, err := s.q.GetCommentByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("comment get by id: %w", err)
	}
	return mapDBCommentToModel(&result), nil
}

func (s *CommentStore) Update(ctx context.Context, id int64, body string) (*model.Comment, error) {
	result, err := s.q.UpdateComment(ctx, id, body)
	if err != nil {
		return nil, fmt.Errorf("comment update: %w", err)
	}
	return mapDBCommentToModel(&result), nil
}

func (s *CommentStore) ListByIssue(ctx context.Context, issueID int64) ([]model.Comment, error) {
	comments, err := s.q.ListCommentsByIssue(ctx, sql.NullInt64{Int64: issueID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("comment list by issue: %w", err)
	}
	return mapDBCommentsToModel(comments), nil
}

func (s *CommentStore) ListByPull(ctx context.Context, pullID int64) ([]model.Comment, error) {
	comments, err := s.q.ListCommentsByPull(ctx, sql.NullInt64{Int64: pullID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("comment list by pull: %w", err)
	}
	return mapDBCommentsToModel(comments), nil
}

func (s *CommentStore) Delete(ctx context.Context, id int64) error {
	return s.q.DeleteComment(ctx, id)
}

func mapDBCommentToModel(dbComment *db.Comment) *model.Comment {
	c := &model.Comment{
		ID:         dbComment.ID,
		RepoID:     dbComment.RepoID,
		AuthorID:   dbComment.AuthorID,
		AuthorName: dbComment.AuthorName,
		Body:       dbComment.Body,
		CreatedAt:  dbComment.CreatedAt,
		UpdatedAt:  dbComment.UpdatedAt,
	}
	if dbComment.IssueID.Valid {
		c.IssueID = &dbComment.IssueID.Int64
	}
	if dbComment.PullID.Valid {
		c.PullID = &dbComment.PullID.Int64
	}
	return c
}

func mapDBCommentsToModel(dbComments []db.Comment) []model.Comment {
	comments := make([]model.Comment, len(dbComments))
	for i, dbComment := range dbComments {
		comments[i] = *mapDBCommentToModel(&dbComment)
	}
	return comments
}
