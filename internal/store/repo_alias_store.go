package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// dbExec is what Add and Drop need from a *sql.DB or a *sql.Tx, so a caller's
// transaction can include them.
type dbExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RepoAliasStore provides database operations for repo_aliases.
type RepoAliasStore struct {
	db *sql.DB
}

// NewRepoAliasStore creates a RepoAliasStore backed by the given database.
func NewRepoAliasStore(database *sql.DB) *RepoAliasStore {
	return &RepoAliasStore{db: database}
}

// Resolve returns the live repo an alias path points at, or sql.ErrNoRows. It
// never returns another alias, and an alias of a soft-deleted repo does not
// resolve.
func (s *RepoAliasStore) Resolve(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	var repoID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT a.repo_id
		   FROM repo_aliases a
		   JOIN repositories r ON r.id = a.repo_id AND r.deleted_at IS NULL
		  WHERE lower(a.owner_name) = lower($1) AND lower(a.name) = lower($2)`,
		ownerName, name,
	).Scan(&repoID)
	if err != nil {
		return nil, fmt.Errorf("repo alias resolve: %w", err)
	}
	return NewRepoStore(s.db).GetByID(ctx, repoID)
}

// Add points ownerName/name at repoID, replacing an alias another repo held
// there: the newer one wins.
func (s *RepoAliasStore) Add(ctx context.Context, repoID int64, ownerName, name string) error {
	return AddRepoAlias(ctx, s.db, repoID, ownerName, name)
}

// AddRepoAlias is Add inside a caller's transaction.
func AddRepoAlias(ctx context.Context, x dbExec, repoID int64, ownerName, name string) error {
	_, err := x.ExecContext(ctx,
		`INSERT INTO repo_aliases (repo_id, owner_name, name) VALUES ($1, $2, $3)
		 ON CONFLICT (lower(owner_name), lower(name))
		 DO UPDATE SET repo_id = EXCLUDED.repo_id, owner_name = EXCLUDED.owner_name, name = EXCLUDED.name, created_at = NOW()`,
		repoID, ownerName, name,
	)
	if err != nil {
		return fmt.Errorf("repo alias add: %w", err)
	}
	return nil
}

// Drop removes the alias at ownerName/name, whoever holds it; none is not an error.
func (s *RepoAliasStore) Drop(ctx context.Context, ownerName, name string) error {
	return DropRepoAlias(ctx, s.db, ownerName, name)
}

// DropRepoAlias is Drop inside a caller's transaction.
func DropRepoAlias(ctx context.Context, x dbExec, ownerName, name string) error {
	_, err := x.ExecContext(ctx,
		`DELETE FROM repo_aliases WHERE lower(owner_name) = lower($1) AND lower(name) = lower($2)`,
		ownerName, name,
	)
	if err != nil {
		return fmt.Errorf("repo alias drop: %w", err)
	}
	return nil
}

// DropByRepo removes every alias of repoID.
func (s *RepoAliasStore) DropByRepo(ctx context.Context, repoID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM repo_aliases WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("repo alias drop by repo: %w", err)
	}
	return nil
}
