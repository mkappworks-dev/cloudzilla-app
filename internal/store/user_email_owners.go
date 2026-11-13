package store

import (
	"context"
	"fmt"
	"strings"
)

// VerifiedEmailOwners maps each lower-cased address in emails to the account that has verified it, in two queries. An
// address verified nowhere is left out; the unique indexes on users.email and user_emails.email make the owner unique.
func (s *UserStore) VerifiedEmailOwners(ctx context.Context, emails []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(emails) == 0 {
		return out, nil
	}
	ph := make([]string, len(emails))
	args := make([]any, len(emails))
	for i, e := range emails {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = strings.ToLower(e)
	}
	in := strings.Join(ph, ",")
	for _, q := range []string{
		`SELECT lower(email), id FROM users WHERE email_verified_at IS NOT NULL AND ` + notGhost + ` AND lower(email) IN (` + in + `)`,
		`SELECT lower(email), user_id FROM user_emails WHERE verified_at IS NOT NULL AND lower(email) IN (` + in + `)`,
	} {
		if err := s.scanEmailOwners(ctx, q, args, out); err != nil {
			return nil, fmt.Errorf("verified email owners: %w", err)
		}
	}
	return out, nil
}

func (s *UserStore) scanEmailOwners(ctx context.Context, query string, args []any, out map[string]int64) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var email string
		var id int64
		if err := rows.Scan(&email, &id); err != nil {
			return err
		}
		out[email] = id
	}
	return rows.Err()
}
