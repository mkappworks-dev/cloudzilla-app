package store

import (
	"context"
	"fmt"
)

// SignerIdentity is what commit-signature verification needs to know about an account.
type SignerIdentity struct {
	ID       int64
	Username string
	// Email is the primary address, set only when it is verified.
	Email string
	// ExtraEmails are the verified addresses in user_emails.
	ExtraEmails []string
}

// SignerIdentities loads the accounts with the given IDs and their verified addresses in two queries.
func (s *UserStore) SignerIdentities(ctx context.Context, ids []int64) (map[int64]*SignerIdentity, error) {
	out := map[int64]*SignerIdentity{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := inPlaceholders(1, ids)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, CASE WHEN email_verified_at IS NULL THEN '' ELSE email END FROM users WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("signer identities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var si SignerIdentity
		if err := rows.Scan(&si.ID, &si.Username, &si.Email); err != nil {
			return nil, err
		}
		out[si.ID] = &si
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	extra, err := s.db.QueryContext(ctx,
		`SELECT user_id, email FROM user_emails WHERE verified_at IS NOT NULL AND user_id IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("signer identity emails: %w", err)
	}
	defer extra.Close()
	for extra.Next() {
		var id int64
		var email string
		if err := extra.Scan(&id, &email); err != nil {
			return nil, err
		}
		if si := out[id]; si != nil {
			si.ExtraEmails = append(si.ExtraEmails, email)
		}
	}
	return out, extra.Err()
}
