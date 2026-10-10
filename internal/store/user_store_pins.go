package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func formatPGInt64Array(ids []int64) string {
	if len(ids) == 0 {
		return "{}"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func parsePGInt64Array(s string) ([]int64, error) {
	s = strings.Trim(s, "{}")
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// AddPinnedRepo drops the prune IDs and appends repoID under a row lock, so
// overlapping pins can't overwrite each other. It reports false, changing
// nothing, when the pins left after pruning already number limit.
func (s *UserStore) AddPinnedRepo(ctx context.Context, userID, repoID int64, prune []int64, limit int) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("user add pinned repo: begin: %w", err)
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRowContext(ctx,
		`SELECT pinned_repo_ids::text FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&raw); err != nil {
		return false, fmt.Errorf("user add pinned repo: lock: %w", err)
	}
	ids, err := parsePGInt64Array(raw)
	if err != nil {
		return false, fmt.Errorf("user add pinned repo: parse: %w", err)
	}
	if slices.Contains(ids, repoID) {
		return true, nil
	}
	ids = slices.DeleteFunc(ids, func(id int64) bool { return slices.Contains(prune, id) })
	if len(ids) >= limit {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET pinned_repo_ids = $2::bigint[], updated_at = NOW() WHERE id = $1`,
		userID, formatPGInt64Array(append(ids, repoID)),
	); err != nil {
		return false, fmt.Errorf("user add pinned repo: update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("user add pinned repo: commit: %w", err)
	}
	return true, nil
}

func (s *UserStore) RemovePinnedRepo(ctx context.Context, userID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET pinned_repo_ids = array_remove(pinned_repo_ids, $2::bigint), updated_at = NOW()
		 WHERE id = $1 AND $2::bigint = ANY(pinned_repo_ids)`,
		userID, repoID,
	)
	if err != nil {
		return fmt.Errorf("user remove pinned repo: %w", err)
	}
	return nil
}

func (s *UserStore) GetPinnedRepoIDs(ctx context.Context, userID int64) ([]int64, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT pinned_repo_ids::text FROM users WHERE id = $1`, userID,
	).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("user get pinned repo ids: %w", err)
	}
	return parsePGInt64Array(raw)
}
