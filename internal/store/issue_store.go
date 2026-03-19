package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type IssueStore struct{ q *db.Queries }

func NewIssueStore(q *db.Queries) *IssueStore { return &IssueStore{q: q} }

func (s *IssueStore) Create(ctx context.Context, issue *model.Issue) error {
	// Get next number for repo
	num, err := s.q.GetNextIssueNumber(ctx, issue.RepoID)
	if err != nil {
		return fmt.Errorf("issue next num: %w", err)
	}
	issue.Number = int(num)

	result, err := s.q.CreateIssue(ctx, db.CreateIssueParams{
		RepoID:   issue.RepoID,
		Number:   int32(issue.Number),
		AuthorID: issue.AuthorID,
		Title:    issue.Title,
		Body:     issue.Body,
		State:    string(issue.State),
	})
	if err != nil {
		return fmt.Errorf("issue create: %w", err)
	}
	issue.ID = result.ID
	issue.CreatedAt = result.CreatedAt
	issue.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *IssueStore) List(ctx context.Context, repoID int64) ([]model.Issue, error) {
	issues, err := s.q.ListIssues(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("issue list: %w", err)
	}
	return mapDBIssuesToModel(issues), nil
}

func (s *IssueStore) GetByNumber(ctx context.Context, repoID int64, number int) (*model.Issue, error) {
	result, err := s.q.GetIssue(ctx, db.GetIssueParams{
		RepoID: repoID,
		Number: int32(number),
	})
	if err != nil {
		return nil, fmt.Errorf("issue get: %w", err)
	}
	return mapDBIssueToModel(&result), nil
}

func (s *IssueStore) UpdateState(ctx context.Context, id int64, state model.IssueState) error {
	now := time.Now().UTC()
	if state == model.IssueStateClosed {
		return s.q.UpdateIssueStateClosed(ctx, db.UpdateIssueStateClosedParams{
			State:     string(state),
			ClosedAt:  sql.NullTime{Time: now, Valid: true},
			UpdatedAt: now,
			ID:        id,
		})
	}
	return s.q.UpdateIssueStateOpen(ctx, db.UpdateIssueStateOpenParams{
		State:     string(state),
		UpdatedAt: now,
		ID:        id,
	})
}

func mapDBIssueToModel(dbIssue *db.Issue) *model.Issue {
	issue := &model.Issue{
		ID:        dbIssue.ID,
		RepoID:    dbIssue.RepoID,
		Number:    int(dbIssue.Number),
		AuthorID:  dbIssue.AuthorID,
		Title:     dbIssue.Title,
		Body:      dbIssue.Body,
		State:     model.IssueState(dbIssue.State),
		CreatedAt: dbIssue.CreatedAt,
		UpdatedAt: dbIssue.UpdatedAt,
	}
	if dbIssue.ClosedAt.Valid {
		issue.ClosedAt = &dbIssue.ClosedAt.Time
	}
	return issue
}

func mapDBIssuesToModel(dbIssues []db.Issue) []model.Issue {
	issues := make([]model.Issue, len(dbIssues))
	for i, dbIssue := range dbIssues {
		issues[i] = *mapDBIssueToModel(&dbIssue)
	}
	return issues
}
