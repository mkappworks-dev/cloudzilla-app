package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// BranchProtectionStore provides database operations for branch protection rules.
type BranchProtectionStore struct{ db *sql.DB }

// NewBranchProtectionStore creates a BranchProtectionStore backed by the given database.
func NewBranchProtectionStore(db *sql.DB) *BranchProtectionStore {
	return &BranchProtectionStore{db: db}
}

func (s *BranchProtectionStore) Create(ctx context.Context, bp *model.BranchProtection) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO branch_protections
		 (repo_id, pattern, require_review_count, require_status_checks, block_force_push, require_pull_request)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at, updated_at`,
		bp.RepoID, bp.Pattern, bp.RequireReviewCount, bp.RequireStatusChecks, bp.BlockForcePush, bp.RequirePullRequest,
	).Scan(&bp.ID, &bp.CreatedAt, &bp.UpdatedAt)
	if err != nil {
		return fmt.Errorf("branch protection create: %w", err)
	}
	return nil
}

func (s *BranchProtectionStore) ListByRepo(ctx context.Context, repoID int64) ([]*model.BranchProtection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, pattern, require_review_count, require_status_checks, block_force_push, require_pull_request, created_at, updated_at
		 FROM branch_protections WHERE repo_id = $1 ORDER BY created_at ASC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("branch protection list: %w", err)
	}
	defer rows.Close()
	var rules []*model.BranchProtection
	for rows.Next() {
		bp := &model.BranchProtection{}
		if err := rows.Scan(
			&bp.ID, &bp.RepoID, &bp.Pattern, &bp.RequireReviewCount,
			&bp.RequireStatusChecks, &bp.BlockForcePush, &bp.RequirePullRequest, &bp.CreatedAt, &bp.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, bp)
	}
	return rules, rows.Err()
}

func (s *BranchProtectionStore) GetByID(ctx context.Context, id int64) (*model.BranchProtection, error) {
	bp := &model.BranchProtection{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, pattern, require_review_count, require_status_checks, block_force_push, require_pull_request, created_at, updated_at
		 FROM branch_protections WHERE id = $1`,
		id,
	).Scan(
		&bp.ID, &bp.RepoID, &bp.Pattern, &bp.RequireReviewCount,
		&bp.RequireStatusChecks, &bp.BlockForcePush, &bp.RequirePullRequest, &bp.CreatedAt, &bp.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("branch protection get: %w", err)
	}
	return bp, nil
}

func (s *BranchProtectionStore) Update(ctx context.Context, id int64, requireReviewCount int, requireStatusChecks model.StringSlice, blockForcePush, requirePullRequest bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE branch_protections
		 SET require_review_count=$2, require_status_checks=$3, block_force_push=$4, require_pull_request=$5, updated_at=NOW()
		 WHERE id=$1`,
		id, requireReviewCount, requireStatusChecks, blockForcePush, requirePullRequest,
	)
	return err
}

func (s *BranchProtectionStore) Delete(ctx context.Context, id, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM branch_protections WHERE id=$1 AND repo_id=$2`,
		id, repoID,
	)
	return err
}

// warnedPatterns dedupes the malformed-pattern warning: MatchForBranch runs on every push.
var warnedPatterns sync.Map

// MatchForBranch returns the first protection rule whose pattern matches branchName, or nil.
func (s *BranchProtectionStore) MatchForBranch(ctx context.Context, repoID int64, branchName string) (*model.BranchProtection, error) {
	rules, err := s.ListByRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		matched, err := filepath.Match(rule.Pattern, branchName)
		if err != nil {
			// Rows stored before create validated patterns; a migration to fix them is deferred.
			if _, seen := warnedPatterns.LoadOrStore(rule.Pattern, struct{}{}); !seen {
				slog.Warn("branch protection rule skipped: malformed pattern", "rule_id", rule.ID, "repo_id", repoID, "pattern", rule.Pattern)
			}
			continue
		}
		if matched {
			return rule, nil
		}
	}
	return nil, nil
}
