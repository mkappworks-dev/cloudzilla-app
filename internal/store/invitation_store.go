package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type InvitationStore struct {
	db *sql.DB
}

func NewInvitationStore(db *sql.DB) *InvitationStore {
	return &InvitationStore{db: db}
}

func (s *InvitationStore) Create(ctx context.Context, inv *model.Invitation) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO invitations (token, email, invited_by_id, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		inv.Token, inv.Email, inv.InvitedByID, inv.ExpiresAt, inv.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("invitation create: %w", err)
	}
	inv.ID, _ = res.LastInsertId()
	return nil
}

func (s *InvitationStore) GetByToken(ctx context.Context, token string) (*model.Invitation, error) {
	inv := &model.Invitation{}
	var acceptedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, token, email, invited_by_id, expires_at, accepted_at, created_at
		 FROM invitations WHERE token = ?`,
		token,
	).Scan(&inv.ID, &inv.Token, &inv.Email, &inv.InvitedByID, &inv.ExpiresAt, &acceptedAt, &inv.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invitation get by token: %w", err)
	}
	if acceptedAt.Valid {
		t := acceptedAt.Time
		inv.AcceptedAt = &t
	}
	return inv, nil
}

func (s *InvitationStore) ListAll(ctx context.Context) ([]model.Invitation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, token, email, invited_by_id, expires_at, accepted_at, created_at
		 FROM invitations ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("invitation list: %w", err)
	}
	defer rows.Close()
	var invs []model.Invitation
	for rows.Next() {
		var inv model.Invitation
		var acceptedAt sql.NullTime
		if err := rows.Scan(&inv.ID, &inv.Token, &inv.Email, &inv.InvitedByID, &inv.ExpiresAt, &acceptedAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		if acceptedAt.Valid {
			t := acceptedAt.Time
			inv.AcceptedAt = &t
		}
		invs = append(invs, inv)
	}
	return invs, rows.Err()
}

func (s *InvitationStore) MarkAccepted(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE invitations SET accepted_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("invitation mark accepted: %w", err)
	}
	return nil
}

func (s *InvitationStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM invitations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("invitation delete: %w", err)
	}
	return nil
}
