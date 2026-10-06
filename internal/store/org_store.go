package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrOrgNameTaken = errors.New("name already taken")

// ErrLastOrgOwner: an org with no owner can be neither managed nor deleted.
var ErrLastOrgOwner = errors.New("organization would have no owner")

// OrgStore provides database operations for organizations and their membership.
type OrgStore struct{ db *sql.DB }

// NewOrgStore creates an OrgStore backed by the given database.
func NewOrgStore(db *sql.DB) *OrgStore { return &OrgStore{db: db} }

func (s *OrgStore) Create(ctx context.Context, o *model.Organization) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO organizations (name, display_name, description, avatar_url)
		 SELECT $1, $2, $3, $4 WHERE NOT `+ownerNameTakenCond+` RETURNING id`,
		o.Name, o.DisplayName, o.Description, o.AvatarURL,
	).Scan(&o.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOrgNameTaken
	}
	if err != nil {
		return fmt.Errorf("org create: %w", err)
	}
	return nil
}

func (s *OrgStore) GetByName(ctx context.Context, name string) (*model.Organization, error) {
	o := &model.Organization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, avatar_url, avatar_key, website, location, contact_email, default_repo_visibility, default_branch_name, created_at, updated_at FROM organizations WHERE name = $1`,
		name,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.AvatarKey, &o.Website, &o.Location, &o.ContactEmail, &o.DefaultRepoVisibility, &o.DefaultBranchName, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("org get by name: %w", err)
	}
	return o, nil
}

func (s *OrgStore) GetByID(ctx context.Context, id int64) (*model.Organization, error) {
	o := &model.Organization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, avatar_url, avatar_key, website, location, contact_email, default_repo_visibility, default_branch_name, created_at, updated_at FROM organizations WHERE id = $1`,
		id,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.AvatarKey, &o.Website, &o.Location, &o.ContactEmail, &o.DefaultRepoVisibility, &o.DefaultBranchName, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("org get by id: %w", err)
	}
	return o, nil
}

func (s *OrgStore) UpdateRepoDefaults(ctx context.Context, id int64, visibility, branchName string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE organizations
		   SET default_repo_visibility = $1,
		       default_branch_name     = $2,
		       updated_at              = NOW()
		 WHERE id = $3`,
		visibility, branchName, id,
	)
	if err != nil {
		return fmt.Errorf("org update repo defaults: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrOrgHasRepos: the org_id cascade would silently wipe every live repo,
// issue, PR and comment of the org.
var ErrOrgHasRepos = errors.New("organization still has repositories")

// Delete removes an org with no live repos and returns its soft-deleted ones,
// whose rows the cascade drops. The org row lock holds off a repo created or
// transferred into the org (their FK checks need that row) until the check
// below has seen it; the repo row locks do the same for a restore. avatarKey is
// read under the same lock.
func (s *OrgStore) Delete(ctx context.Context, id int64) (deleted []model.Repository, avatarKey string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", fmt.Errorf("org delete begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT avatar_key FROM organizations WHERE id = $1 FOR UPDATE`, id).Scan(&avatarKey); err != nil {
		return nil, "", fmt.Errorf("org delete lock: %w", err)
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id, owner_name, name, deleted_at FROM repositories WHERE org_id = $1 FOR UPDATE`, id)
	if err != nil {
		return nil, "", fmt.Errorf("org delete list repos: %w", err)
	}
	hasLive := false
	for rows.Next() {
		r := model.Repository{OrgID: id}
		var deletedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.OwnerName, &r.Name, &deletedAt); err != nil {
			rows.Close()
			return nil, "", fmt.Errorf("org delete scan repo: %w", err)
		}
		if !deletedAt.Valid {
			hasLive = true
			continue
		}
		r.DeletedAt = &deletedAt.Time
		deleted = append(deleted, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("org delete list repos: %w", err)
	}
	if hasLive {
		return nil, "", ErrOrgHasRepos
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, id); err != nil {
		return nil, "", fmt.Errorf("org delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("org delete commit: %w", err)
	}
	return deleted, avatarKey, nil
}

// UpdateProfile returns sql.ErrNoRows when no row matches id.
func (s *OrgStore) UpdateProfile(ctx context.Context, id int64, displayName, description, website, location, contactEmail string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE organizations
		   SET display_name  = $1,
		       description   = $2,
		       website       = $3,
		       location      = $4,
		       contact_email = $5,
		       updated_at    = NOW()
		 WHERE id = $6`,
		displayName, description, website, location, contactEmail, id,
	)
	if err != nil {
		return fmt.Errorf("org update profile: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *OrgStore) AddMember(ctx context.Context, orgID, userID int64, role model.OrgRole) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID, userID, string(role),
	)
	if err != nil {
		return fmt.Errorf("org add member: %w", err)
	}
	return nil
}

func (s *OrgStore) RemoveMember(ctx context.Context, orgID, userID int64) error {
	return s.changeMember(ctx, orgID, userID, true,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`)
}

// changeMember applies change with the org row locked, refusing one that takes
// away the last owner. UserStore.DeleteWithOwnedRepos takes the same lock, so
// two owners leaving, being demoted or deleting their accounts at once cannot
// each count the other as the one who stays.
func (s *OrgStore) changeMember(ctx context.Context, orgID, userID int64, dropsOwner bool, change string, args ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("org member change begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM organizations WHERE id = $1 FOR NO KEY UPDATE`, orgID); err != nil {
		return fmt.Errorf("org member change lock: %w", err)
	}
	if dropsOwner {
		var last bool
		err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2 AND role = 'owner')
			    AND NOT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id <> $2 AND role = 'owner')`,
			orgID, userID,
		).Scan(&last)
		if err != nil {
			return fmt.Errorf("org member change count owners: %w", err)
		}
		if last {
			return ErrLastOrgOwner
		}
	}
	if _, err := tx.ExecContext(ctx, change, append([]any{orgID, userID}, args...)...); err != nil {
		return fmt.Errorf("org member change: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("org member change commit: %w", err)
	}
	return nil
}

func (s *OrgStore) GetMember(ctx context.Context, orgID, userID int64) (*model.OrgMember, error) {
	m := &model.OrgMember{}
	err := s.db.QueryRowContext(ctx,
		`SELECT om.id, om.org_id, om.user_id, u.username, om.role, om.created_at
		 FROM org_members om JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1 AND om.user_id = $2`,
		orgID, userID,
	).Scan(&m.ID, &m.OrgID, &m.UserID, &m.Username, &m.Role, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("org get member: %w", err)
	}
	return m, nil
}

func (s *OrgStore) ListMembers(ctx context.Context, orgID int64) ([]model.OrgMember, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT om.id, om.org_id, om.user_id, u.username, om.role, om.created_at
		 FROM org_members om JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1 ORDER BY om.created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("org list members: %w", err)
	}
	defer rows.Close()
	var members []model.OrgMember
	for rows.Next() {
		var m model.OrgMember
		if err := rows.Scan(&m.ID, &m.OrgID, &m.UserID, &m.Username, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *OrgStore) CountMembers(ctx context.Context, orgID int64) (int, error) {
	var c int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org_id = $1`,
		orgID,
	).Scan(&c)
	if err != nil {
		return 0, fmt.Errorf("org count members: %w", err)
	}
	return c, nil
}

func (s *OrgStore) UpdateMemberRole(ctx context.Context, orgID, userID int64, role model.OrgRole) error {
	return s.changeMember(ctx, orgID, userID, role != model.OrgRoleOwner,
		`UPDATE org_members SET role = $3 WHERE org_id = $1 AND user_id = $2`, string(role))
}

func (s *OrgStore) ListByMember(ctx context.Context, userID int64) ([]model.Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.display_name, o.description, o.avatar_url, o.avatar_key, o.website, o.location, o.contact_email, o.default_repo_visibility, o.default_branch_name, o.created_at, o.updated_at
		 FROM organizations o JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = $1 ORDER BY o.name ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("org list by member: %w", err)
	}
	defer rows.Close()
	var orgs []model.Organization
	for rows.Next() {
		var o model.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.AvatarKey, &o.Website, &o.Location, &o.ContactEmail, &o.DefaultRepoVisibility, &o.DefaultBranchName, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

// SwapAvatarKey sets the org's avatar key and returns the one it replaced,
// read under the row lock. It returns sql.ErrNoRows when the org doesn't exist.
func (s *OrgStore) SwapAvatarKey(ctx context.Context, orgID int64, key string) (string, error) {
	var old string
	err := s.db.QueryRowContext(ctx,
		`UPDATE organizations o SET avatar_key = $2, updated_at = NOW()
		   FROM (SELECT id, avatar_key FROM organizations WHERE id = $1 FOR UPDATE) prev
		  WHERE o.id = prev.id
		 RETURNING prev.avatar_key`, orgID, key,
	).Scan(&old)
	if err != nil {
		return "", fmt.Errorf("org swap avatar key: %w", err)
	}
	return old, nil
}
