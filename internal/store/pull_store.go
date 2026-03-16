package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type PullStore struct{ q *db.Queries }

func NewPullStore(q *db.Queries) *PullStore { return &PullStore{q: q} }

func (s *PullStore) Create(ctx context.Context, pr *model.PullRequest) error {
	num, err := s.q.GetNextPullNumber(ctx, pr.RepoID)
	if err != nil {
		return fmt.Errorf("pr next num: %w", err)
	}
	pr.Number = int(num)

	result, err := s.q.CreatePull(ctx, db.CreatePullParams{
		RepoID:     pr.RepoID,
		Number:     int64(pr.Number),
		AuthorID:   pr.AuthorID,
		Title:      pr.Title,
		Body:       pr.Body,
		State:      string(pr.State),
		HeadBranch: pr.HeadBranch,
		BaseBranch: pr.BaseBranch,
	})
	if err != nil {
		return fmt.Errorf("pr create: %w", err)
	}
	pr.ID = result.ID
	pr.CreatedAt = result.CreatedAt
	pr.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *PullStore) List(ctx context.Context, repoID int64) ([]model.PullRequest, error) {
	prs, err := s.q.ListPulls(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("pr list: %w", err)
	}
	return mapDBPullsToModel(prs), nil
}

func (s *PullStore) GetByNumber(ctx context.Context, repoID int64, number int) (*model.PullRequest, error) {
	result, err := s.q.GetPull(ctx, db.GetPullParams{
		RepoID: repoID,
		Number: int64(number),
	})
	if err != nil {
		return nil, fmt.Errorf("pr get: %w", err)
	}
	return mapDBPullToModel(&result), nil
}

func (s *PullStore) UpdateState(ctx context.Context, id int64, state model.PRState) error {
	now := time.Now().UTC()
	switch state {
	case model.PRStateMerged:
		return s.q.UpdatePullStateMerged(ctx, db.UpdatePullStateMergedParams{
			State:     string(state),
			MergedAt:  sql.NullTime{Time: now, Valid: true},
			UpdatedAt: now,
			ID:        id,
		})
	case model.PRStateClosed:
		return s.q.UpdatePullStateClosed(ctx, db.UpdatePullStateClosedParams{
			State:     string(state),
			ClosedAt:  sql.NullTime{Time: now, Valid: true},
			UpdatedAt: now,
			ID:        id,
		})
	default:
		return s.q.UpdatePullStateOpen(ctx, db.UpdatePullStateOpenParams{
			State:     string(state),
			UpdatedAt: now,
			ID:        id,
		})
	}
}

func mapDBPullToModel(dbPull *db.PullRequest) *model.PullRequest {
	pr := &model.PullRequest{
		ID:         dbPull.ID,
		RepoID:     dbPull.RepoID,
		Number:     int(dbPull.Number),
		AuthorID:   dbPull.AuthorID,
		Title:      dbPull.Title,
		Body:       dbPull.Body,
		State:      model.PRState(dbPull.State),
		HeadBranch: dbPull.HeadBranch,
		BaseBranch: dbPull.BaseBranch,
		CreatedAt:  dbPull.CreatedAt,
		UpdatedAt:  dbPull.UpdatedAt,
	}
	if dbPull.MergedAt.Valid {
		pr.MergedAt = &dbPull.MergedAt.Time
	}
	if dbPull.ClosedAt.Valid {
		pr.ClosedAt = &dbPull.ClosedAt.Time
	}
	return pr
}

func mapDBPullsToModel(dbPulls []db.PullRequest) []model.PullRequest {
	prs := make([]model.PullRequest, len(dbPulls))
	for i, dbPull := range dbPulls {
		prs[i] = *mapDBPullToModel(&dbPull)
	}
	return prs
}
