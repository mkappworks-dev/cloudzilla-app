package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type PullStore struct {
	q  *db.Queries
	db *sql.DB
}

func NewPullStore(q *db.Queries, database *sql.DB) *PullStore {
	return &PullStore{q: q, db: database}
}

func (s *PullStore) Create(ctx context.Context, pr *model.PullRequest) error {
	num, err := s.q.GetNextPullNumber(ctx, pr.RepoID)
	if err != nil {
		return fmt.Errorf("pr next num: %w", err)
	}
	pr.Number = int(num)

	result, err := s.q.CreatePull(ctx, db.CreatePullParams{
		RepoID:     pr.RepoID,
		Number:     int32(pr.Number),
		AuthorID:   pr.AuthorID,
		Title:      pr.Title,
		Body:       pr.Body,
		State:      string(pr.State),
		HeadBranch: pr.HeadBranch,
		BaseBranch: pr.BaseBranch,
		IsDraft:    pr.IsDraft,
	})
	if err != nil {
		return fmt.Errorf("pr create: %w", err)
	}
	pr.ID = result.ID
	pr.CreatedAt = result.CreatedAt
	pr.UpdatedAt = result.UpdatedAt
	pr.IsDraft = result.IsDraft
	if result.DraftAt.Valid {
		pr.DraftAt = &result.DraftAt.Time
	}
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
		Number: int32(number),
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

func (s *PullStore) SetDraft(ctx context.Context, id int64, isDraft bool) error {
	return s.q.UpdatePullDraft(ctx, db.UpdatePullDraftParams{
		IsDraft: isDraft,
		ID:      id,
	})
}

func (s *PullStore) SetAutoMerge(ctx context.Context, id int64, enabled bool, strategy string) error {
	var strat sql.NullString
	if strategy != "" {
		strat = sql.NullString{String: strategy, Valid: true}
	}
	return s.q.SetAutoMerge(ctx, db.SetAutoMergeParams{
		ID:                id,
		AutoMergeEnabled:  enabled,
		AutoMergeStrategy: strat,
	})
}

func (s *PullStore) ListOpen(ctx context.Context, repoID int64) ([]model.PullRequest, error) {
	prs, err := s.q.ListOpen(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("pr list open: %w", err)
	}
	return mapDBPullsToModel(prs), nil
}

func (s *PullStore) GetByID(ctx context.Context, id int64) (*model.PullRequest, error) {
	result, err := s.q.GetPullByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("pr get by id: %w", err)
	}
	return mapDBPullToModel(&result), nil
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
	pr.IsDraft = dbPull.IsDraft
	if dbPull.DraftAt.Valid {
		pr.DraftAt = &dbPull.DraftAt.Time
	}
	pr.AutoMergeEnabled = dbPull.AutoMergeEnabled
	if dbPull.AutoMergeStrategy.Valid {
		pr.AutoMergeStrategy = dbPull.AutoMergeStrategy.String
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

func (s *PullStore) CountCreatedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1 AND created_at >= $2`,
		repoID, since,
	).Scan(&n)
	return n, err
}

func (s *PullStore) CountMergedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1 AND state = 'merged' AND merged_at >= $2`,
		repoID, since,
	).Scan(&n)
	return n, err
}

func (s *PullStore) CountOpen(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1 AND state = 'open'`,
		repoID,
	).Scan(&n)
	return n, err
}
