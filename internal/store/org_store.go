package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type OrgStore struct{ db *sql.DB }

func NewOrgStore(db *sql.DB) *OrgStore { return &OrgStore{db: db} }

func (s *OrgStore) Create(ctx context.Context, o *model.Organization) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (name, display_name, description, avatar_url) VALUES (?, ?, ?, ?)`,
		o.Name, o.DisplayName, o.Description, o.AvatarURL,
	)
	if err != nil {
		return fmt.Errorf("org create: %w", err)
	}
	o.ID, _ = res.LastInsertId()
	return nil
}

func (s *OrgStore) GetByName(ctx context.Context, name string) (*model.Organization, error) {
	o := &model.Organization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, avatar_url, created_at, updated_at FROM organizations WHERE name = ?`,
		name,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("org get by name: %w", err)
	}
	return o, nil
}

func (s *OrgStore) GetByID(ctx context.Context, id int64) (*model.Organization, error) {
	o := &model.Organization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, avatar_url, created_at, updated_at FROM organizations WHERE id = ?`,
		id,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("org get by id: %w", err)
	}
	return o, nil
}

func (s *OrgStore) AddMember(ctx context.Context, orgID, userID int64, role model.OrgRole) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)`,
		orgID, userID, string(role),
	)
	if err != nil {
		return fmt.Errorf("org add member: %w", err)
	}
	return nil
}

func (s *OrgStore) RemoveMember(ctx context.Context, orgID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM org_members WHERE org_id = ? AND user_id = ?`,
		orgID, userID,
	)
	return err
}

func (s *OrgStore) GetMember(ctx context.Context, orgID, userID int64) (*model.OrgMember, error) {
	m := &model.OrgMember{}
	err := s.db.QueryRowContext(ctx,
		`SELECT om.id, om.org_id, om.user_id, u.username, om.role, om.created_at
		 FROM org_members om JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = ? AND om.user_id = ?`,
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
		 WHERE om.org_id = ? ORDER BY om.created_at ASC`,
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

func (s *OrgStore) UpdateMemberRole(ctx context.Context, orgID, userID int64, role model.OrgRole) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_members SET role = ? WHERE org_id = ? AND user_id = ?`,
		string(role), orgID, userID,
	)
	return err
}

func (s *OrgStore) ListByMember(ctx context.Context, userID int64) ([]model.Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.display_name, o.description, o.avatar_url, o.created_at, o.updated_at
		 FROM organizations o JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = ? ORDER BY o.name ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("org list by member: %w", err)
	}
	defer rows.Close()
	var orgs []model.Organization
	for rows.Next() {
		var o model.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}
