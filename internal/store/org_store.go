package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// OrgStore provides database operations for organizations and their membership.
type OrgStore struct{ db *sql.DB }

// NewOrgStore creates an OrgStore backed by the given database.
func NewOrgStore(db *sql.DB) *OrgStore { return &OrgStore{db: db} }

func (s *OrgStore) Create(ctx context.Context, o *model.Organization) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO organizations (name, display_name, description, avatar_url) VALUES ($1, $2, $3, $4) RETURNING id`,
		o.Name, o.DisplayName, o.Description, o.AvatarURL,
	).Scan(&o.ID)
	if err != nil {
		return fmt.Errorf("org create: %w", err)
	}
	return nil
}

func (s *OrgStore) GetByName(ctx context.Context, name string) (*model.Organization, error) {
	o := &model.Organization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, avatar_url, website, location, contact_email, default_repo_visibility, default_branch_name, created_at, updated_at FROM organizations WHERE name = $1`,
		name,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.Website, &o.Location, &o.ContactEmail, &o.DefaultRepoVisibility, &o.DefaultBranchName, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("org get by name: %w", err)
	}
	return o, nil
}

func (s *OrgStore) GetByID(ctx context.Context, id int64) (*model.Organization, error) {
	o := &model.Organization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, avatar_url, website, location, contact_email, default_repo_visibility, default_branch_name, created_at, updated_at FROM organizations WHERE id = $1`,
		id,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.Website, &o.Location, &o.ContactEmail, &o.DefaultRepoVisibility, &o.DefaultBranchName, &o.CreatedAt, &o.UpdatedAt)
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

// Delete removes the organization row. The org_members rows cascade via FK.
// Callers must ensure there are no repositories under the org first, otherwise
// FK ON DELETE CASCADE will silently wipe every repo, issue, PR, and comment
// belonging to it.
func (s *OrgStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("org delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	)
	return err
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
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_members SET role = $1 WHERE org_id = $2 AND user_id = $3`,
		string(role), orgID, userID,
	)
	return err
}

func (s *OrgStore) ListByMember(ctx context.Context, userID int64) ([]model.Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.display_name, o.description, o.avatar_url, o.website, o.location, o.contact_email, o.default_repo_visibility, o.default_branch_name, o.created_at, o.updated_at
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
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.Website, &o.Location, &o.ContactEmail, &o.DefaultRepoVisibility, &o.DefaultBranchName, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}
