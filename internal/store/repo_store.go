package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ErrRepoNameInUse signals a unique violation on (owner_id, name), or on
// (org_id, name) among live org repos.
var ErrRepoNameInUse = errors.New("repository name already in use")

// ErrRepoChanged: another request moved, deleted or restored the repo since
// the caller read it.
var ErrRepoChanged = errors.New("repository changed while the request ran; reload and try again")

func repoWriteErr(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrRepoNameInUse
	}
	return fmt.Errorf("%s: %w", op, err)
}

// nullID stores zero as NULL: an org repo has no owner_id, a personal repo no org_id.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// zeroIfNull scans a nullable ID as zero, which matches no user.
type zeroIfNull struct{ dst *int64 }

func (z zeroIfNull) Scan(src any) error {
	var n sql.NullInt64
	if err := n.Scan(src); err != nil {
		return err
	}
	*z.dst = n.Int64
	return nil
}

// ownedBy is RepoService.IsOwner as a SQL predicate on repositories alias r
// for the user ID in placeholder u.
func ownedBy(r, u string) string {
	return `(` + r + `.owner_id = ` + u +
		` OR EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = ` + r + `.org_id AND om.user_id = ` + u + ` AND om.role = 'owner'))`
}

// readableBy is RepoService.CanRead as a SQL predicate, for queries that
// filter many repos at once.
func readableBy(r, u string) string {
	return `(NOT ` + r + `.private OR ` + ownedBy(r, u) +
		` OR EXISTS (SELECT 1 FROM permissions perm WHERE perm.repo_id = ` + r + `.id AND perm.user_id = ` + u + `))`
}

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
		`INSERT INTO repositories (owner_id, created_by, name, description, private, default_branch)
		 VALUES ($1, $1, $2, $3, $4, $5)
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
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, org_id, created_by, name, description, private, default_branch, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		nullID(r.OwnerID), r.OwnerName, nullID(r.OrgID), nullID(r.CreatedBy), r.Name, r.Description, r.Private, r.DefaultBranch, now, now,
	).Scan(&r.ID)
	if err != nil {
		return repoWriteErr("repo create with owner name", err)
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
		`SELECT id, owner_id, owner_name, org_id, name, description, website, license, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template,
		        allow_issues, allow_discussions, allow_projects, allow_wiki, created_by
		 FROM repositories WHERE owner_name = $1 AND name = $2 AND deleted_at IS NULL`,
		ownerName, name,
	).Scan(&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Website, &r.License, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt,
		&r.IsFork, &forkOfID, &r.ForkCount, &r.IsArchived, &archivedAt, &r.IsTemplate,
		&r.AllowIssues, &r.AllowDiscussions, &r.AllowProjects, &r.AllowWiki, zeroIfNull{&r.CreatedBy})
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

// NameHeld counts soft-deleted rows too, and ignores the case of both names
// as a case-insensitive filesystem would.
func (s *RepoStore) NameHeld(ctx context.Context, ownerName, name string) (bool, error) {
	var held bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM repositories WHERE lower(owner_name) = lower($1) AND lower(name) = lower($2))`,
		ownerName, name,
	).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("repo name held: %w", err)
	}
	return held, nil
}

func (s *RepoStore) GetByOwnerNameList(ctx context.Context, ownerName string) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template, primary_language
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
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template, primary_language
		 FROM repositories WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("repo list: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

// Ordered by ID for stable iteration in background jobs; no per-user / per-org filtering.
func (s *RepoStore) ListAll(ctx context.Context) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template, primary_language
		 FROM repositories WHERE deleted_at IS NULL ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("repo list all: %w", err)
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
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template, primary_language
		 FROM repositories WHERE owner_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list by owner: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

// ListAllByOwnerID includes soft-deleted repos, which deleting the owner
// cascades away too. Only the fields that locate a repo on disk are set.
func (s *RepoStore) ListAllByOwnerID(ctx context.Context, ownerID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_name, name, deleted_at FROM repositories WHERE owner_id = $1`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list all by owner: %w", err)
	}
	defer rows.Close()
	var repos []model.Repository
	for rows.Next() {
		r := model.Repository{OwnerID: ownerID}
		var deletedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.OwnerName, &r.Name, &deletedAt); err != nil {
			return nil, err
		}
		if deletedAt.Valid {
			r.DeletedAt = &deletedAt.Time
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *RepoStore) GetByOrgID(ctx context.Context, orgID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template, primary_language
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

func (s *RepoStore) UpdateGeneral(ctx context.Context, repoID int64, description, website, defaultBranch string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET description = $1, website = $2, default_branch = $3, updated_at = $4 WHERE id = $5`,
		description, website, defaultBranch, time.Now().UTC(), repoID,
	)
	if err != nil {
		return fmt.Errorf("update repo general settings: %w", err)
	}
	return nil
}

func (s *RepoStore) UpdateFeatureToggles(ctx context.Context, repoID int64, issues, discussions, projects, wiki bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET allow_issues = $1, allow_discussions = $2, allow_projects = $3, allow_wiki = $4, updated_at = $5 WHERE id = $6`,
		issues, discussions, projects, wiki, time.Now().UTC(), repoID,
	)
	if err != nil {
		return fmt.Errorf("update repo feature toggles: %w", err)
	}
	return nil
}

func (s *RepoStore) UpdateVisibility(ctx context.Context, repoID int64, private bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET private = $1, updated_at = NOW() WHERE id = $2`,
		private, repoID,
	)
	if err != nil {
		return fmt.Errorf("update repo visibility: %w", err)
	}
	return nil
}

// UpdateOwner hands a live repo still under oldOwnerName to a user
// (newOrgID zero) or an org (newOwnerID zero).
func (s *RepoStore) UpdateOwner(ctx context.Context, repoID int64, oldOwnerName string, newOwnerID, newOrgID int64, newOwnerName string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET owner_id = $1, org_id = $2, owner_name = $3, updated_at = $4
		 WHERE id = $5 AND owner_name = $6 AND deleted_at IS NULL`,
		nullID(newOwnerID), nullID(newOrgID), newOwnerName, time.Now().UTC(), repoID, oldOwnerName,
	)
	if err != nil {
		return repoWriteErr("update repo owner", err)
	}
	return requireRow(res, "update repo owner")
}

func requireRow(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", op, err)
	}
	if n == 0 {
		return ErrRepoChanged
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

func (s *RepoStore) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.Permission, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, user_id, role, created_at FROM permissions WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list permissions by user: %w", err)
	}
	defer rows.Close()
	var perms []model.Permission
	for rows.Next() {
		var p model.Permission
		if err := rows.Scan(&p.ID, &p.RepoID, &p.UserID, &p.Role, &p.CreatedAt); err != nil {
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
		CreatedBy:     newOwnerID,
		OwnerName:     newOwnerName,
		Name:          newName,
		Description:   orig.Description,
		Private:       orig.Private,
		DefaultBranch: orig.DefaultBranch,
		IsFork:        true,
		ForkOfID:      &orig.ID,
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, created_by, owner_name, name, description, private, default_branch, is_fork, fork_of_id, created_at, updated_at)
		 VALUES ($1, $1, $2, $3, $4, $5, $6, TRUE, $7, $8, $9) RETURNING id`,
		r.OwnerID, r.OwnerName, r.Name, r.Description, r.Private, r.DefaultBranch, orig.ID, now, now,
	).Scan(&r.ID)
	if err != nil {
		return nil, repoWriteErr("repo fork", err)
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

func (s *RepoStore) ListForks(ctx context.Context, repoID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template, primary_language
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
	var primaryLang sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, website, license, private, default_branch, created_at, updated_at,
		        is_fork, fork_of_id, fork_count, is_archived, archived_at, is_template,
		        allow_issues, allow_discussions, allow_projects, allow_wiki, primary_language, created_by
		 FROM repositories WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Website, &r.License, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt,
		&r.IsFork, &forkOfID, &r.ForkCount, &r.IsArchived, &archivedAt, &r.IsTemplate,
		&r.AllowIssues, &r.AllowDiscussions, &r.AllowProjects, &r.AllowWiki, &primaryLang, zeroIfNull{&r.CreatedBy})
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
	if primaryLang.Valid {
		r.PrimaryLanguage = &primaryLang.String
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

func (s *RepoStore) UpdatePrimaryLanguage(ctx context.Context, repoID int64, lang string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET primary_language = $2, updated_at = $3 WHERE id = $1`,
		repoID, lang, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("update repo primary language: %w", err)
	}
	return nil
}

// FillPrimaryLanguage writes only a NULL column, so a value computed from an
// older tree can't clobber one a push wrote meanwhile, including a push's ”
// for "no code". It leaves updated_at alone so a page view can't reorder
// recently-updated lists.
func (s *RepoStore) FillPrimaryLanguage(ctx context.Context, repoID int64, lang string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET primary_language = $2
		 WHERE id = $1 AND primary_language IS NULL`,
		repoID, lang,
	)
	if err != nil {
		return fmt.Errorf("fill repo primary language: %w", err)
	}
	return nil
}

func (s *RepoStore) UpdateMeta(ctx context.Context, repoID int64, description, website, license string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET description = $1, website = $2, license = $3, updated_at = $4 WHERE id = $5`,
		description, website, license, time.Now().UTC(), repoID,
	)
	if err != nil {
		return fmt.Errorf("update repo meta: %w", err)
	}
	return nil
}

func (s *RepoStore) ListTemplates(ctx context.Context) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch,
		        created_at, updated_at, is_fork, fork_of_id, fork_count,
		        is_archived, archived_at, is_template, primary_language
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

// Delete soft-deletes a live repo still under ownerName.
func (s *RepoStore) Delete(ctx context.Context, repoID int64, ownerName string, deletedByID int64, deletedAt time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET deleted_at = $3, deleted_by = $2, updated_at = NOW()
		 WHERE id = $1 AND owner_name = $4 AND deleted_at IS NULL`,
		repoID, deletedByID, deletedAt, ownerName,
	)
	if err != nil {
		return fmt.Errorf("soft delete repo: %w", err)
	}
	return requireRow(res, "soft delete repo")
}

func (s *RepoStore) Restore(ctx context.Context, repoID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET deleted_at = NULL, deleted_by = NULL, updated_at = NOW() WHERE id = $1 AND deleted_at IS NOT NULL`,
		repoID,
	)
	if err != nil {
		return repoWriteErr("restore repo", err)
	}
	return requireRow(res, "restore repo")
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
	).Scan(&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
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
	).Scan(&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
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
			&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
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
	defer tx.Rollback()

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
			&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch,
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

// Org-owned repos are not counted.
func (s *RepoStore) CountForUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM repositories
		 WHERE owner_id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&n)
	return n, err
}

// scope is "owned", "collaborator", or "all" (default for any unknown value).
// The repo owner also holds a permission row, so "collaborator" excludes owned repos.
func (s *RepoStore) ListForUser(ctx context.Context, userID int64, scope string) ([]model.Repository, error) {
	const cols = `r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private, r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id, r.fork_count, r.is_archived, r.archived_at, r.is_template, r.primary_language`
	var where string
	switch scope {
	case "owned":
		where = `r.owner_id = $1`
	case "collaborator":
		where = `r.owner_id IS DISTINCT FROM $1 AND EXISTS (SELECT 1 FROM permissions p WHERE p.repo_id = r.id AND p.user_id = $1)`
	default: // "all"
		where = `(r.owner_id = $1 OR EXISTS (SELECT 1 FROM permissions p WHERE p.repo_id = r.id AND p.user_id = $1))`
	}
	q := `SELECT ` + cols + ` FROM repositories r WHERE r.deleted_at IS NULL AND (` + where + `) ORDER BY r.updated_at DESC`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("repo list for user: %w", err)
	}
	defer rows.Close()
	return scanRepoRows(rows)
}

func scanRepoRows(rows *sql.Rows) ([]model.Repository, error) {
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID, forkOfID sql.NullInt64
		var archivedAt sql.NullTime
		var primaryLang sql.NullString
		if err := rows.Scan(&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt,
			&r.IsFork, &forkOfID, &r.ForkCount, &r.IsArchived, &archivedAt, &r.IsTemplate, &primaryLang); err != nil {
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
		if primaryLang.Valid {
			r.PrimaryLanguage = &primaryLang.String
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}
