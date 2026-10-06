package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	// ErrLastSuperadmin is returned by any change that would leave the instance
	// with no active superadmin, and so with no way back short of SQL.
	ErrLastSuperadmin = errors.New("no active superadmin would remain")
	ErrUserSuspended  = errors.New("user is suspended")
)

const activeSuperadminCond = `is_superadmin AND suspended_at IS NULL AND ` + notGhost

// keepActiveSuperadmin locks every active superadmin row and fails with
// ErrLastSuperadmin when userID is the only one. Concurrent removals queue on
// the locks, and the loser re-reads the rows the winner changed, so two admins
// demoting each other can't both succeed. NO KEY UPDATE still lets rows that
// reference these users be written meanwhile.
func keepActiveSuperadmin(ctx context.Context, tx *sql.Tx, userID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE `+activeSuperadminCond+` ORDER BY id FOR NO KEY UPDATE`)
	if err != nil {
		return fmt.Errorf("lock active superadmins: %w", err)
	}
	defer rows.Close()
	var target bool
	var others int
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("lock active superadmins: %w", err)
		}
		if id == userID {
			target = true
		} else {
			others++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("lock active superadmins: %w", err)
	}
	if target && others == 0 {
		return ErrLastSuperadmin
	}
	return nil
}

// inTx runs fn in a transaction that commits only if fn succeeds.
func (s *UserStore) inTx(ctx context.Context, op string, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	return nil
}

// Suspend suspends userID and ends their sessions in one statement, so
// unsuspending doesn't revive them. It reports false when already suspended.
func (s *UserStore) Suspend(ctx context.Context, userID int64) (bool, error) {
	var changed bool
	err := s.inTx(ctx, "user suspend", func(tx *sql.Tx) error {
		if err := keepActiveSuperadmin(ctx, tx, userID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE users SET suspended_at = NOW(), session_version = session_version + 1, updated_at = NOW()
			 WHERE id = $1 AND suspended_at IS NULL AND `+notGhost, userID)
		if err != nil {
			return fmt.Errorf("user suspend: %w", err)
		}
		n, err := res.RowsAffected()
		changed = n == 1
		return err
	})
	return changed, err
}

// Unsuspend reports false when the user wasn't suspended.
func (s *UserStore) Unsuspend(ctx context.Context, userID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET suspended_at = NULL, updated_at = NOW() WHERE id = $1 AND suspended_at IS NOT NULL`, userID)
	if err != nil {
		return false, fmt.Errorf("user unsuspend: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Promote fails with ErrUserSuspended for a suspended user. The check is in
// the UPDATE itself, so a suspension that lands first wins.
func (s *UserStore) Promote(ctx context.Context, userID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_superadmin = TRUE, updated_at = NOW() WHERE id = $1 AND suspended_at IS NULL AND `+notGhost, userID)
	if err != nil {
		return fmt.Errorf("user promote: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("user promote: %w", err)
	} else if n == 0 {
		return ErrUserSuspended
	}
	return nil
}

func (s *UserStore) Demote(ctx context.Context, userID int64) error {
	return s.inTx(ctx, "user demote", func(tx *sql.Tx) error {
		if err := keepActiveSuperadmin(ctx, tx, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET is_superadmin = FALSE, updated_at = NOW() WHERE id = $1`, userID); err != nil {
			return fmt.Errorf("user demote: %w", err)
		}
		return nil
	})
}

// RevokeCredentials deletes userID's personal access tokens, SSH keys and
// OAuth app authorizations and ends their sessions. Deploy keys stay: they
// belong to repositories, not to the user.
func (s *UserStore) RevokeCredentials(ctx context.Context, userID int64) (model.RevokedCredentials, error) {
	var out model.RevokedCredentials
	err := s.inTx(ctx, "user revoke credentials", func(tx *sql.Tx) error {
		for _, d := range []struct {
			table string
			n     *int64
		}{
			{"access_tokens", &out.AccessTokens},
			{"ssh_keys", &out.SSHKeys},
			{"oauth_authorizations", &out.OAuthAuthorizations},
		} {
			res, err := tx.ExecContext(ctx, `DELETE FROM `+d.table+` WHERE user_id = $1`, userID)
			if err != nil {
				return fmt.Errorf("user revoke %s: %w", d.table, err)
			}
			if *d.n, err = res.RowsAffected(); err != nil {
				return fmt.Errorf("user revoke %s: %w", d.table, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET session_version = session_version + 1, updated_at = NOW() WHERE id = $1`, userID); err != nil {
			return fmt.Errorf("user revoke sessions: %w", err)
		}
		return nil
	})
	return out, err
}

// SoleOwnedOrgNames lists, by name, the orgs userID is the only owner of.
func (s *UserStore) SoleOwnedOrgNames(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.name FROM organizations o WHERE EXISTS (`+soleOwnedOrgs+` AND om.org_id = o.id) ORDER BY o.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("user sole owned orgs: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("user sole owned orgs: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// likePrefix escapes LIKE's wildcards so q matches only as a literal prefix.
func likePrefix(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
}

// ListForAdmin returns one page of accounts, newest first, and the total that
// match f. The ghost is never listed.
func (s *UserStore) ListForAdmin(ctx context.Context, f model.AdminUserFilter, page, perPage int) ([]model.AdminUserRow, int, error) {
	where := []string{notGhost}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		p := arg(likePrefix(q))
		where = append(where, `(lower(username) LIKE `+p+` OR lower(email) LIKE `+p+`)`)
	}
	switch f.Role {
	case model.AdminUserRoleSuperadmin:
		where = append(where, `is_superadmin`)
	case model.AdminUserRoleUser:
		where = append(where, `NOT is_superadmin`)
	}
	switch f.Status {
	case model.AdminUserStatusActive:
		where = append(where, `suspended_at IS NULL`)
	case model.AdminUserStatusSuspended:
		where = append(where, `suspended_at IS NOT NULL`)
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("user list for admin count: %w", err)
	}
	if page < 1 {
		page = 1
	}
	limit, offset := arg(perPage), arg((page-1)*perPage)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+`, totp_enabled, password_hash <> '', COALESCE(sso_provider, '')
		 FROM users WHERE `+cond+` ORDER BY created_at DESC, id DESC LIMIT `+limit+` OFFSET `+offset, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("user list for admin: %w", err)
	}
	defer rows.Close()
	var out []model.AdminUserRow
	for rows.Next() {
		var r model.AdminUserRow
		if err := scanUser(rows, &r.User, &r.User.TOTPEnabled, &r.HasPassword, &r.SSOProvider); err != nil {
			return nil, 0, fmt.Errorf("user list for admin scan: %w", err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// GetForAdmin loads one account for the admin user page; the ghost is a not-found.
func (s *UserStore) GetForAdmin(ctx context.Context, username string) (*model.AdminUserRow, error) {
	var r model.AdminUserRow
	err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+`, totp_enabled, password_hash <> '', COALESCE(sso_provider, '')
		 FROM users WHERE username = $1 AND `+notGhost, username),
		&r.User, &r.User.TOTPEnabled, &r.HasPassword, &r.SSOProvider)
	if err != nil {
		return nil, fmt.Errorf("user get for admin: %w", err)
	}
	return &r, nil
}

// IsLastActiveSuperadmin is an unlocked early check; the changes that matter
// re-check under keepActiveSuperadmin.
func (s *UserStore) IsLastActiveSuperadmin(ctx context.Context, userID int64) (bool, error) {
	var last bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND `+activeSuperadminCond+`)
		    AND NOT EXISTS (SELECT 1 FROM users WHERE id <> $1 AND `+activeSuperadminCond+`)`, userID).Scan(&last)
	if err != nil {
		return false, fmt.Errorf("user is last active superadmin: %w", err)
	}
	return last, nil
}
