package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// LinkOAuthByVerifiedEmail re-checks the conditions in the UPDATE itself, so an
// email change or another link that lands after the caller's read wins; it then
// returns sql.ErrNoRows.
func (s *UserStore) LinkOAuthByVerifiedEmail(ctx context.Context, userID int64, email, provider, oauthID string) (*model.User, error) {
	u := &model.User{}
	err := scanUser(s.db.QueryRowContext(ctx,
		`UPDATE users SET oauth_provider = $3, oauth_id = $4, updated_at = NOW()
		 WHERE id = $1 AND email = $2 AND email_verified_at IS NOT NULL AND oauth_provider = '' AND password_hash <> ''
		 RETURNING `+userColumns,
		userID, email, provider, oauthID,
	), u)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == usersOAuthKey {
			return nil, ErrOAuthIdentityTaken
		}
		return nil, fmt.Errorf("user link oauth by verified email: %w", err)
	}
	return u, nil
}

// SSOLink returns the LDAP or SAML identity userID signs in with, if any.
func (s *UserStore) SSOLink(ctx context.Context, userID int64) (provider, ssoID string, err error) {
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(sso_provider, ''), COALESCE(sso_id, '') FROM users WHERE id = $1`, userID,
	).Scan(&provider, &ssoID); err != nil {
		return "", "", fmt.Errorf("user sso link: %w", err)
	}
	return provider, ssoID, nil
}

// ChangePassword swaps oldHash for newHash and ends every session issued before
// the change, in one statement. It reports false, changing nothing, when the
// hash is no longer oldHash, so a confirmation only replaces the password it checked.
func (s *UserStore) ChangePassword(ctx context.Context, userID int64, oldHash, newHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = $3, session_version = session_version + 1, updated_at = NOW()
		 WHERE id = $1 AND password_hash = $2`,
		userID, oldHash, newHash)
	if err != nil {
		return false, fmt.Errorf("user change password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("user change password rows: %w", err)
	}
	return n == 1, nil
}

// BumpSessionVersion ends every session issued before it and returns the new version.
func (s *UserStore) BumpSessionVersion(ctx context.Context, userID int64) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET session_version = session_version + 1, updated_at = NOW() WHERE id = $1 RETURNING session_version`,
		userID,
	).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("user bump session version: %w", err)
	}
	return v, nil
}

// SessionState returns sql.ErrNoRows for a suspended user, so their sessions die
// on the next request.
func (s *UserStore) SessionState(ctx context.Context, userID int64) (model.SessionState, error) {
	var st model.SessionState
	if err := s.db.QueryRowContext(ctx,
		`SELECT session_version, is_superadmin FROM users WHERE id = $1 AND suspended_at IS NULL`, userID,
	).Scan(&st.Version, &st.IsSuperadmin); err != nil {
		return model.SessionState{}, fmt.Errorf("user session state: %w", err)
	}
	return st, nil
}

func (s *UserStore) SessionVersion(ctx context.Context, userID int64) (int, error) {
	var v int
	if err := s.db.QueryRowContext(ctx, `SELECT session_version FROM users WHERE id = $1`, userID).Scan(&v); err != nil {
		return 0, fmt.Errorf("user session version: %w", err)
	}
	return v, nil
}

// MarkEmailVerified verifies email only while it is still userID's address,
// and reports whether it was.
func (s *UserStore) MarkEmailVerified(ctx context.Context, userID int64, email string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW()), updated_at = NOW()
		 WHERE id = $1 AND lower(email) = lower($2)`,
		userID, email,
	)
	if err != nil {
		return false, fmt.Errorf("user mark email verified: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("user mark email verified: %w", err)
	}
	return n == 1, nil
}

// LinkOAuth links an account that has no provider link yet, reporting false if it already has one.
func (s *UserStore) LinkOAuth(ctx context.Context, userID int64, provider, oauthID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET oauth_provider = $2, oauth_id = $3, updated_at = NOW()
		 WHERE id = $1 AND oauth_provider = ''`,
		userID, provider, oauthID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == usersOAuthKey {
			return false, ErrOAuthIdentityTaken
		}
		return false, fmt.Errorf("user link oauth: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("user link oauth: %w", err)
	}
	return n == 1, nil
}

// UnlinkOAuth clears the account's link to provider, reporting false if there was none.
// An account without a password keeps its link: it is the only way to sign in to it.
func (s *UserStore) UnlinkOAuth(ctx context.Context, userID int64, provider string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET oauth_provider = '', oauth_id = '', updated_at = NOW()
		 WHERE id = $1 AND oauth_provider = $2 AND password_hash <> ''`,
		userID, provider,
	)
	if err != nil {
		return false, fmt.Errorf("user unlink oauth: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("user unlink oauth: %w", err)
	}
	return n == 1, nil
}

// LinkSSO sets sso_provider and sso_id on an existing user account.
// Used when an SSO login matches an existing local account by email.
func (s *UserStore) LinkSSO(ctx context.Context, userID int64, provider, ssoID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET sso_provider = $1, sso_id = $2, updated_at = NOW() WHERE id = $3`,
		provider, ssoID, userID,
	)
	if err != nil {
		return fmt.Errorf("user link sso: %w", err)
	}
	return nil
}
