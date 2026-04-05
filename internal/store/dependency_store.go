package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type DependencyStore struct{ db *sql.DB }

func NewDependencyStore(db *sql.DB) *DependencyStore { return &DependencyStore{db: db} }

// Replace atomically replaces all dependencies for a repo in a single transaction.
// It deletes all existing rows for the repo then inserts the new set.
func (s *DependencyStore) Replace(ctx context.Context, repoID int64, deps []model.RepoDependency) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dependency replace begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM repo_dependencies WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("dependency replace delete: %w", err)
	}

	if len(deps) == 0 {
		return tx.Commit()
	}

	// Insert in chunks to stay within PostgreSQL's 65535-parameter limit.
	// With 5 params per row, 1000 rows = 5000 parameters per statement.
	const chunkSize = 1000
	for start := 0; start < len(deps); start += chunkSize {
		end := start + chunkSize
		if end > len(deps) {
			end = len(deps)
		}
		chunk := deps[start:end]

		placeholders := make([]string, len(chunk))
		args := make([]interface{}, 0, len(chunk)*5)
		for i, d := range chunk {
			base := i * 5
			placeholders[i] = fmt.Sprintf("($%d, $%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4, base+5)
			args = append(args, repoID, d.PackageMgr, d.Package, d.Version, d.IsDev)
		}

		q := `INSERT INTO repo_dependencies (repo_id, package_mgr, package, version, is_dev) VALUES ` +
			strings.Join(placeholders, ", ") +
			` ON CONFLICT (repo_id, package_mgr, package) DO UPDATE SET version = EXCLUDED.version, is_dev = EXCLUDED.is_dev, updated_at = NOW()`

		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("dependency replace insert: %w", err)
		}
	}

	return tx.Commit()
}

// ListByRepo returns all dependencies for a repository, ordered by package manager then package name.
func (s *DependencyStore) ListByRepo(ctx context.Context, repoID int64) ([]model.RepoDependency, error) {
	const q = `
SELECT id, repo_id, package_mgr, package, version, is_dev, updated_at
FROM repo_dependencies
WHERE repo_id = $1
ORDER BY package_mgr, package`

	rows, err := s.db.QueryContext(ctx, q, repoID)
	if err != nil {
		return nil, fmt.Errorf("list dependencies: %w", err)
	}
	defer rows.Close()

	var deps []model.RepoDependency
	for rows.Next() {
		var d model.RepoDependency
		if err := rows.Scan(&d.ID, &d.RepoID, &d.PackageMgr, &d.Package, &d.Version, &d.IsDev, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan dependency row: %w", err)
		}
		deps = append(deps, d)
	}
	return deps, rows.Err()
}
