package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// RepoStore provides database operations for repositories and their permissions.
type RepoStore struct {
	db *sql.DB
}

// NewRepoStore creates a RepoStore backed by the given database.
func NewRepoStore(database *sql.DB) *RepoStore {
	return &RepoStore{db: database}
}

func (s *RepoStore) Create(ctx context.Context, r *model.Repository) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, name, description, private, default_branch)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at, updated_at`,
		r.OwnerID, r.Name, r.Description, r.Private, r.DefaultBranch,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repo create: %w", err)
	}
	return nil
}

func (s *RepoStore) CreateWithOwnerName(ctx context.Context, r *model.Repository) error {
	now := time.Now().UTC()
	var orgID interface{}
	if r.OrgID != 0 {
		orgID = r.OrgID
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		r.OwnerID, r.OwnerName, orgID, r.Name, r.Description, r.Private, r.DefaultBranch, now, now,
	).Scan(&r.ID)
	if err != nil {
		return fmt.Errorf("repo create with owner name: %w", err)
	}
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (s *RepoStore) GetByOwnerName(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	r := &model.Repository{}
	var orgID, forkOfID sql.NullInt64
	var archivedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE owner_name = $1 AND name = $2 AND deleted_at IS NULL`,
		ownerName, name,
	).Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt,
		&r.IsFork, &forkOfID, &r.ForkCount, &r.IsArchived, &archivedAt, &r.IsTemplate)
	if err != nil {
		return nil, fmt.Errorf("repo get by owner name: %w", err)
	}
	if orgID.Valid {
		r.OrgID = orgID.Int64
	}
	if forkOfID.Valid {
		r.ForkOfID = &forkOfID.Int64
	}
	if archivedAt.Valid {
		r.ArchivedAt = &archivedAt.Time
	}
	return r, nil
}

func (s *RepoStore) GetByOwnerNameList(ctx context.Context, ownerName string) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE owner_name = $1 AND deleted_at IS NULL ORDER BY created_at DESC`,
		ownerName,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list by owner name: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func (s *RepoStore) List(ctx context.Context) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("repo list: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func (s *RepoStore) GetByOwnerAndName(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	return s.GetByOwnerName(ctx, ownerName, name)
}

func (s *RepoStore) GetByOwnerID(ctx context.Context, ownerID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE owner_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list by owner: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func (s *RepoStore) GetByOrgID(ctx context.Context, orgID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE org_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list by org: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func (s *RepoStore) GetPermission(ctx context.Context, repoID, userID int64) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM permissions WHERE repo_id = $1 AND user_id = $2`,
		repoID, userID,
	).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("get permission: %w", err)
	}
	return role, nil
}

func (s *RepoStore) UpdateOwner(ctx context.Context, repoID, newOwnerID int64, newOwnerName string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET owner_id = $1, owner_name = $2, updated_at = $3 WHERE id = $4`,
		newOwnerID, newOwnerName, time.Now().UTC(), repoID,
	)
	if err != nil {
		return fmt.Errorf("update repo owner: %w", err)
	}
	return nil
}

func (s *RepoStore) AddPermission(ctx context.Context, repoID, userID int64, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO permissions (repo_id, user_id, role, created_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT(repo_id, user_id) DO UPDATE SET role = excluded.role`,
		repoID, userID, role,
	)
	if err != nil {
		return fmt.Errorf("add permission: %w", err)
	}
	return nil
}

func (s *RepoStore) UpdatePermission(ctx context.Context, repoID, userID int64, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE permissions SET role = $1 WHERE repo_id = $2 AND user_id = $3`,
		role, repoID, userID,
	)
	if err != nil {
		return fmt.Errorf("update permission: %w", err)
	}
	return nil
}

func (s *RepoStore) RemovePermission(ctx context.Context, repoID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM permissions WHERE repo_id = $1 AND user_id = $2`,
		repoID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove permission: %w", err)
	}
	return nil
}

func (s *RepoStore) ListPermissionsWithUsername(ctx context.Context, repoID int64) ([]model.Permission, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.repo_id, p.user_id, p.role, u.username, p.created_at
		 FROM permissions p
		 JOIN users u ON p.user_id = u.id
		 WHERE p.repo_id = $1
		 ORDER BY p.created_at ASC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	defer rows.Close()
	var perms []model.Permission
	for rows.Next() {
		var p model.Permission
		if err := rows.Scan(&p.ID, &p.RepoID, &p.UserID, &p.Role, &p.Username, &p.CreatedAt); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

func (s *RepoStore) Fork(ctx context.Context, orig *model.Repository, newOwnerID int64, newOwnerName, newName string) (*model.Repository, error) {
	now := time.Now().UTC()
	r := &model.Repository{
		OwnerID:       newOwnerID,
		OwnerName:     newOwnerName,
		Name:          newName,
		Description:   orig.Description,
		Private:       orig.Private,
		DefaultBranch: orig.DefaultBranch,
		IsFork:        true,
		ForkOfID:      &orig.ID,
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch, is_fork, fork_of_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, $8, $9) RETURNING id`,
		r.OwnerID, r.OwnerName, r.Name, r.Description, r.Private, r.DefaultBranch, orig.ID, now, now,
	).Scan(&r.ID)
	if err != nil {
		return nil, fmt.Errorf("repo fork: %w", err)
	}
	r.CreatedAt = now
	r.UpdatedAt = now
	return r, nil
}

func (s *RepoStore) IncrementForkCount(ctx context.Context, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET fork_count = fork_count + 1 WHERE id = $1`,
		repoID,
	)
	if err != nil {
		return fmt.Errorf("increment fork count: %w", err)
	}
	return nil
}

func (s *RepoStore) DecrementForkCount(ctx context.Context, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET fork_count = GREATEST(fork_count - 1, 0) WHERE id = $1`,
		repoID,
	)
	if err != nil {
		return fmt.Errorf("decrement fork count: %w", err)
	}
	return nil
}

func (s *RepoStore) ListForks(ctx context.Context, repoID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE fork_of_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list forks: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func (s *RepoStore) GetByID(ctx context.Context, id int64) (*model.Repository, error) {
	r := &model.Repository{}
	var orgID, forkOfID sql.NullInt64
	var archivedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template
		 FROM repositories WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt,
		&r.IsFork, &forkOfID, &r.ForkCount, &r.IsArchived, &archivedAt, &r.IsTemplate)
	if err != nil {
		return nil, fmt.Errorf("repo get by id: %w", err)
	}
	if orgID.Valid {
		r.OrgID = orgID.Int64
	}
	if forkOfID.Valid {
		r.ForkOfID = &forkOfID.Int64
	}
	if archivedAt.Valid {
		r.ArchivedAt = &archivedAt.Time
	}
	return r, nil
}

func (s *RepoStore) SetArchived(ctx context.Context, repoID int64, archived bool) error {
	var archivedAt interface{}
	if archived {
		archivedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET is_archived = $1, archived_at = $2, updated_at = $3 WHERE id = $4`,
		archived, archivedAt, time.Now().UTC(), repoID,
	)
	if err != nil {
		return fmt.Errorf("set archived: %w", err)
	}
	return nil
}

func (s *RepoStore) SetTemplate(ctx context.Context, repoID int64, isTemplate bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET is_template = $1, updated_at = $2 WHERE id = $3`,
		isTemplate, time.Now().UTC(), repoID,
	)
	if err != nil {
		return fmt.Errorf("set template: %w", err)
	}
	return nil
}

func (s *RepoStore) ListTemplates(ctx context.Context) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch,
		        created_at, updated_at, is_fork, fork_of_id, fork_count,
		        is_archived, archived_at, is_template
		 FROM repositories
		 WHERE is_template = TRUE AND private = FALSE AND is_archived = FALSE AND deleted_at IS NULL
		 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func (s *RepoStore) DeleteByID(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = $1`, id)
	return err
}

func (s *RepoStore) Delete(ctx context.Context, repoID, deletedByID int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET deleted_at = NOW(), deleted_by = $2, updated_at = NOW() WHERE id = $1`,
		repoID, deletedByID,
	)
	if err != nil {
		return fmt.Errorf("soft delete repo: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete repo rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("soft delete repo: repo %d not found or already deleted", repoID)
	}
	return nil
}

func (s *RepoStore) Restore(ctx context.Context, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET deleted_at = NULL, deleted_by = NULL, updated_at = NOW() WHERE id = $1`,
		repoID,
	)
	if err != nil {
		return fmt.Errorf("restore repo: %w", err)
	}
	return nil
}

func (s *RepoStore) GetDeletedByID(ctx context.Context, id int64) (*model.Repository, error) {
	r := &model.Repository{}
	var orgID, forkOfID, deletedBy sql.NullInt64
	var deletedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch,
		        created_at, updated_at, is_fork, fork_of_id, fork_count, deleted_at, deleted_by
		 FROM repositories WHERE id = $1 AND deleted_at IS NOT NULL`,
		id,
	).Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
		&r.CreatedAt, &r.UpdatedAt, &r.IsFork, &forkOfID, &r.ForkCount, &deletedAt, &deletedBy)
	if err != nil {
		return nil, fmt.Errorf("get deleted repo by id: %w", err)
	}
	if orgID.Valid {
		r.OrgID = orgID.Int64
	}
	if forkOfID.Valid {
		r.ForkOfID = &forkOfID.Int64
	}
	if deletedAt.Valid {
		r.DeletedAt = &deletedAt.Time
	}
	if deletedBy.Valid {
		r.DeletedBy = &deletedBy.Int64
	}
	return r, nil
}

func (s *RepoStore) GetDeletedByOwnerAndName(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	r := &model.Repository{}
	var orgID, forkOfID, deletedBy sql.NullInt64
	var deletedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch,
		        created_at, updated_at, is_fork, fork_of_id, fork_count, deleted_at, deleted_by
		 FROM repositories
		 WHERE owner_name = $1 AND name = $2 AND deleted_at IS NOT NULL
		 ORDER BY deleted_at DESC LIMIT 1`,
		ownerName, name,
	).Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
		&r.CreatedAt, &r.UpdatedAt, &r.IsFork, &forkOfID, &r.ForkCount, &deletedAt, &deletedBy)
	if err != nil {
		return nil, fmt.Errorf("get deleted repo: %w", err)
	}
	if orgID.Valid {
		r.OrgID = orgID.Int64
	}
	if forkOfID.Valid {
		r.ForkOfID = &forkOfID.Int64
	}
	if deletedAt.Valid {
		r.DeletedAt = &deletedAt.Time
	}
	if deletedBy.Valid {
		r.DeletedBy = &deletedBy.Int64
	}
	return r, nil
}

func (s *RepoStore) ListDeleted(ctx context.Context, ownerID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch,
		        created_at, updated_at, is_fork, fork_of_id, fork_count, deleted_at, deleted_by
		 FROM repositories
		 WHERE deleted_at IS NOT NULL
		   AND owner_id = $1
		   AND deleted_at > NOW() - INTERVAL '30 days'
		 ORDER BY deleted_at DESC`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list deleted repos: %w", err)
	}
	defer rows.Close()
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID, forkOfID, deletedBy sql.NullInt64
		var deletedAt sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
			&r.CreatedAt, &r.UpdatedAt, &r.IsFork, &forkOfID, &r.ForkCount, &deletedAt, &deletedBy,
		); err != nil {
			return nil, err
		}
		if orgID.Valid {
			r.OrgID = orgID.Int64
		}
		if forkOfID.Valid {
			r.ForkOfID = &forkOfID.Int64
		}
		if deletedAt.Valid {
			r.DeletedAt = &deletedAt.Time
		}
		if deletedBy.Valid {
			r.DeletedBy = &deletedBy.Int64
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *RepoStore) PurgeExpired(ctx context.Context, before time.Time) ([]model.Repository, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("purge expired begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch,
		        created_at, updated_at, is_fork, fork_of_id, fork_count, deleted_at, deleted_by
		 FROM repositories
		 WHERE deleted_at IS NOT NULL AND deleted_at < $1
		 FOR UPDATE SKIP LOCKED`,
		before,
	)
	if err != nil {
		return nil, fmt.Errorf("purge expired select: %w", err)
	}
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID, forkOfID, deletedBy sql.NullInt64
		var deletedAt sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
			&r.CreatedAt, &r.UpdatedAt, &r.IsFork, &forkOfID, &r.ForkCount, &deletedAt, &deletedBy,
		); err != nil {
			rows.Close()
			return nil, err
		}
		if orgID.Valid {
			r.OrgID = orgID.Int64
		}
		if forkOfID.Valid {
			r.ForkOfID = &forkOfID.Int64
		}
		if deletedAt.Valid {
			r.DeletedAt = &deletedAt.Time
		}
		if deletedBy.Valid {
			r.DeletedBy = &deletedBy.Int64
		}
		repos = append(repos, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, r := range repos {
		if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE id = $1`, r.ID); err != nil {
			return nil, fmt.Errorf("purge expired hard delete %d: %w", r.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("purge expired commit: %w", err)
	}
	return repos, nil
}

func scanRepoRows(rows *sql.Rows) ([]model.Repository, error) {
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID, forkOfID sql.NullInt64
		var archivedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt,
			&r.IsFork, &forkOfID, &r.ForkCount, &r.IsArchived, &archivedAt, &r.IsTemplate); err != nil {
			return nil, err
		}
		if orgID.Valid {
			r.OrgID = orgID.Int64
		}
		if forkOfID.Valid {
			r.ForkOfID = &forkOfID.Int64
		}
		if archivedAt.Valid {
			r.ArchivedAt = &archivedAt.Time
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}
