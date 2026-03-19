package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type StarStore struct{ db *sql.DB }

func NewStarStore(db *sql.DB) *StarStore { return &StarStore{db: db} }

func (s *StarStore) Star(ctx context.Context, userID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO stars (user_id, repo_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, repoID,
	)
	return err
}

func (s *StarStore) Unstar(ctx context.Context, userID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM stars WHERE user_id = $1 AND repo_id = $2`,
		userID, repoID,
	)
	return err
}

func (s *StarStore) CountByRepo(ctx context.Context, repoID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stars WHERE repo_id = $1`, repoID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("star count: %w", err)
	}
	return count, nil
}

func (s *StarStore) IsStarred(ctx context.Context, userID, repoID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM stars WHERE user_id = $1 AND repo_id = $2)`,
		userID, repoID,
	).Scan(&exists)
	return exists, err
}

func (s *StarStore) ListByUser(ctx context.Context, userID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private, r.default_branch, r.created_at, r.updated_at
		 FROM repositories r JOIN stars st ON r.id = st.repo_id
		 WHERE st.user_id = $1 AND r.private = false
		 ORDER BY st.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("star list by user: %w", err)
	}
	defer rows.Close()
	return scanRepos(rows)
}

func (s *StarStore) ListStargazers(ctx context.Context, repoID int64) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.email, u.bio, u.avatar_url, u.is_superadmin, u.is_invited, u.created_at, u.updated_at
		 FROM users u JOIN stars st ON u.id = st.user_id
		 WHERE st.repo_id = $1 ORDER BY st.created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("star list stargazers: %w", err)
	}
	defer rows.Close()
	return scanUsers(rows)
}

func scanRepos(rows *sql.Rows) ([]model.Repository, error) {
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID sql.NullInt64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if orgID.Valid {
			r.OrgID = orgID.Int64
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}
