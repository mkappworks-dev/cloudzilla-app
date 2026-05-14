package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// PullStore provides database operations for pull requests.
type PullStore struct {
	db *sql.DB
}

// NewPullStore creates a PullStore backed by the given database.
func NewPullStore(database *sql.DB) *PullStore {
	return &PullStore{db: database}
}

func (s *PullStore) Create(ctx context.Context, pr *model.PullRequest) error {
	var num int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = $1`,
		pr.RepoID,
	).Scan(&num)
	if err != nil {
		return fmt.Errorf("pr next num: %w", err)
	}
	pr.Number = num

	var mergedAt, closedAt, draftAt sql.NullTime
	var autoMergeStrategy sql.NullString
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch, is_draft)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at, updated_at, merged_at, closed_at, is_draft, draft_at, auto_merge_enabled, auto_merge_strategy`,
		pr.RepoID, pr.Number, pr.AuthorID, pr.Title, pr.Body,
		string(pr.State), pr.HeadBranch, pr.BaseBranch, pr.IsDraft,
	).Scan(&pr.ID, &pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
		&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy)
	if err != nil {
		return fmt.Errorf("pr create: %w", err)
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}
	if closedAt.Valid {
		pr.ClosedAt = &closedAt.Time
	}
	if draftAt.Valid {
		pr.DraftAt = &draftAt.Time
	}
	if autoMergeStrategy.Valid {
		pr.AutoMergeStrategy = autoMergeStrategy.String
	}
	return nil
}

func (s *PullStore) List(ctx context.Context, repoID int64) ([]model.PullRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
		        created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
		        auto_merge_enabled, auto_merge_strategy
		 FROM pull_requests WHERE repo_id = $1 ORDER BY number DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("pr list: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}

func (s *PullStore) GetByNumber(ctx context.Context, repoID int64, number int) (*model.PullRequest, error) {
	pr := &model.PullRequest{}
	var mergedAt, closedAt, draftAt sql.NullTime
	var autoMergeStrategy sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
		        created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
		        auto_merge_enabled, auto_merge_strategy
		 FROM pull_requests WHERE repo_id = $1 AND number = $2`,
		repoID, number,
	).Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Title, &pr.Body,
		&pr.State, &pr.HeadBranch, &pr.BaseBranch,
		&pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
		&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy)
	if err != nil {
		return nil, fmt.Errorf("pr get: %w", err)
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}
	if closedAt.Valid {
		pr.ClosedAt = &closedAt.Time
	}
	if draftAt.Valid {
		pr.DraftAt = &draftAt.Time
	}
	if autoMergeStrategy.Valid {
		pr.AutoMergeStrategy = autoMergeStrategy.String
	}
	return pr, nil
}

func (s *PullStore) UpdateState(ctx context.Context, id int64, state model.PRState) error {
	now := time.Now().UTC()
	switch state {
	case model.PRStateMerged:
		_, err := s.db.ExecContext(ctx,
			`UPDATE pull_requests SET state = $1, merged_at = $2, updated_at = $3 WHERE id = $4`,
			string(state), sql.NullTime{Time: now, Valid: true}, now, id,
		)
		return err
	case model.PRStateClosed:
		_, err := s.db.ExecContext(ctx,
			`UPDATE pull_requests SET state = $1, closed_at = $2, updated_at = $3 WHERE id = $4`,
			string(state), sql.NullTime{Time: now, Valid: true}, now, id,
		)
		return err
	default:
		_, err := s.db.ExecContext(ctx,
			`UPDATE pull_requests SET state = $1, updated_at = $2 WHERE id = $3`,
			string(state), now, id,
		)
		return err
	}
}

func (s *PullStore) SetDraft(ctx context.Context, id int64, isDraft bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests
		  SET is_draft = $1,
		      draft_at = CASE WHEN $1 = TRUE THEN NOW() ELSE draft_at END,
		      updated_at = NOW()
		WHERE id = $2`,
		isDraft, id,
	)
	return err
}

func (s *PullStore) SetAutoMerge(ctx context.Context, id int64, enabled bool, strategy string) error {
	var strat sql.NullString
	if strategy != "" {
		strat = sql.NullString{String: strategy, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests
		 SET auto_merge_enabled  = $2,
		     auto_merge_strategy = $3,
		     updated_at          = NOW()
		 WHERE id = $1`,
		id, enabled, strat,
	)
	return err
}

func (s *PullStore) ListOpen(ctx context.Context, repoID int64) ([]model.PullRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
		        created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
		        auto_merge_enabled, auto_merge_strategy
		 FROM pull_requests WHERE repo_id = $1 AND state = 'open' ORDER BY number DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("pr list open: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}

func (s *PullStore) GetByID(ctx context.Context, id int64) (*model.PullRequest, error) {
	pr := &model.PullRequest{}
	var mergedAt, closedAt, draftAt sql.NullTime
	var autoMergeStrategy sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
		        created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
		        auto_merge_enabled, auto_merge_strategy
		 FROM pull_requests WHERE id = $1`,
		id,
	).Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Title, &pr.Body,
		&pr.State, &pr.HeadBranch, &pr.BaseBranch,
		&pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
		&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy)
	if err != nil {
		return nil, fmt.Errorf("pr get by id: %w", err)
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}
	if closedAt.Valid {
		pr.ClosedAt = &closedAt.Time
	}
	if draftAt.Valid {
		pr.DraftAt = &draftAt.Time
	}
	if autoMergeStrategy.Valid {
		pr.AutoMergeStrategy = autoMergeStrategy.String
	}
	return pr, nil
}

func scanPullRows(rows *sql.Rows) ([]model.PullRequest, error) {
	var prs []model.PullRequest
	for rows.Next() {
		var pr model.PullRequest
		var mergedAt, closedAt, draftAt sql.NullTime
		var autoMergeStrategy sql.NullString
		if err := rows.Scan(
			&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Title, &pr.Body,
			&pr.State, &pr.HeadBranch, &pr.BaseBranch,
			&pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
			&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy,
		); err != nil {
			return nil, err
		}
		if mergedAt.Valid {
			pr.MergedAt = &mergedAt.Time
		}
		if closedAt.Valid {
			pr.ClosedAt = &closedAt.Time
		}
		if draftAt.Valid {
			pr.DraftAt = &draftAt.Time
		}
		if autoMergeStrategy.Valid {
			pr.AutoMergeStrategy = autoMergeStrategy.String
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
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

// Excludes soft-deleted repos so home-page counts match the heatmap's visibility rule.
func (s *PullStore) CountOpenAuthoredByOrAssignedTo(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT p.id)
		 FROM pull_requests p
		 JOIN repositories r ON r.id = p.repo_id
		 LEFT JOIN pull_assignees a ON a.pull_id = p.id
		 WHERE p.state = 'open' AND r.deleted_at IS NULL AND (p.author_id = $1 OR a.user_id = $1)`,
		userID,
	).Scan(&n)
	return n, err
}
