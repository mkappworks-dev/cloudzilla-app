package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// CommitStatusStore provides database operations for commit status checks.
type CommitStatusStore struct{ db *sql.DB }

// NewCommitStatusStore creates a CommitStatusStore backed by the given database.
func NewCommitStatusStore(db *sql.DB) *CommitStatusStore { return &CommitStatusStore{db: db} }

func (s *CommitStatusStore) Upsert(ctx context.Context, cs *model.CommitStatus) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO commit_statuses (repo_id, sha, context, state, target_url, description, creator_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (repo_id, sha, context) DO UPDATE
		   SET state=$4, target_url=$5, description=$6, creator_id=$7, updated_at=NOW()
		 RETURNING id, created_at, updated_at`,
		cs.RepoID, cs.SHA, cs.Context, cs.State, cs.TargetURL, cs.Description, cs.CreatorID,
	).Scan(&cs.ID, &cs.CreatedAt, &cs.UpdatedAt)
	if err != nil {
		return fmt.Errorf("commit status upsert: %w", err)
	}
	return nil
}

func (s *CommitStatusStore) ListBySHA(ctx context.Context, repoID int64, sha string) ([]model.CommitStatus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, sha, context, state, target_url, description, creator_id, created_at, updated_at
		 FROM commit_statuses WHERE repo_id = $1 AND sha = $2 ORDER BY context`,
		repoID, sha,
	)
	if err != nil {
		return nil, fmt.Errorf("commit status list: %w", err)
	}
	defer rows.Close()
	var statuses []model.CommitStatus
	for rows.Next() {
		var cs model.CommitStatus
		if err := rows.Scan(&cs.ID, &cs.RepoID, &cs.SHA, &cs.Context, &cs.State, &cs.TargetURL, &cs.Description, &cs.CreatorID, &cs.CreatedAt, &cs.UpdatedAt); err != nil {
			return nil, err
		}
		statuses = append(statuses, cs)
	}
	return statuses, rows.Err()
}

// GetCombined returns the aggregated worst-case state for all contexts on a SHA.
// Returns empty string if no statuses.
func (s *CommitStatusStore) GetCombined(ctx context.Context, repoID int64, sha string) (model.CommitStatusState, error) {
	statuses, err := s.ListBySHA(ctx, repoID, sha)
	if err != nil {
		return "", err
	}
	return CombineStates(statuses), nil
}

// CombineStates returns the worst state among statuses: error > failure > pending > success.
// Returns empty string if statuses is empty.
func CombineStates(statuses []model.CommitStatus) model.CommitStatusState {
	if len(statuses) == 0 {
		return ""
	}
	result := model.CommitStatusSuccess
	for _, cs := range statuses {
		switch cs.State {
		case model.CommitStatusError:
			return model.CommitStatusError
		case model.CommitStatusFailure:
			result = model.CommitStatusFailure
		case model.CommitStatusPending:
			if result != model.CommitStatusFailure {
				result = model.CommitStatusPending
			}
		}
	}
	return result
}

// ListRecentSHAs returns the repo's SHAs that have statuses, most recently updated first.
func (s *CommitStatusStore) ListRecentSHAs(ctx context.Context, repoID int64, limit, offset int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sha FROM commit_statuses WHERE repo_id = $1
		 GROUP BY sha ORDER BY MAX(updated_at) DESC, sha
		 LIMIT $2 OFFSET $3`,
		repoID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("commit status recent shas: %w", err)
	}
	defer rows.Close()
	var shas []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		shas = append(shas, sha)
	}
	return shas, rows.Err()
}

// ListBySHAs returns the statuses for the given SHAs, ordered by sha then context.
func (s *CommitStatusStore) ListBySHAs(ctx context.Context, repoID int64, shas []string) ([]model.CommitStatus, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(shas))
	args := make([]any, 0, len(shas)+1)
	args = append(args, repoID)
	for i, sha := range shas {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, sha)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, sha, context, state, target_url, description, creator_id, created_at, updated_at
		 FROM commit_statuses WHERE repo_id = $1 AND sha IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY sha, context`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("commit status list by shas: %w", err)
	}
	defer rows.Close()
	var statuses []model.CommitStatus
	for rows.Next() {
		var cs model.CommitStatus
		if err := rows.Scan(&cs.ID, &cs.RepoID, &cs.SHA, &cs.Context, &cs.State, &cs.TargetURL, &cs.Description, &cs.CreatorID, &cs.CreatedAt, &cs.UpdatedAt); err != nil {
			return nil, err
		}
		statuses = append(statuses, cs)
	}
	return statuses, rows.Err()
}
