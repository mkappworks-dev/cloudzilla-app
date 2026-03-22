package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type CommitStatusStore struct{ db *sql.DB }

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
// Priority: error > failure > pending > success. Returns empty string if no statuses.
func (s *CommitStatusStore) GetCombined(ctx context.Context, repoID int64, sha string) (model.CommitStatusState, error) {
	statuses, err := s.ListBySHA(ctx, repoID, sha)
	if err != nil {
		return "", err
	}
	if len(statuses) == 0 {
		return "", nil
	}
	result := model.CommitStatusSuccess
	for _, cs := range statuses {
		switch cs.State {
		case model.CommitStatusError:
			return model.CommitStatusError, nil
		case model.CommitStatusFailure:
			result = model.CommitStatusFailure
		case model.CommitStatusPending:
			if result != model.CommitStatusFailure {
				result = model.CommitStatusPending
			}
		}
	}
	return result, nil
}
