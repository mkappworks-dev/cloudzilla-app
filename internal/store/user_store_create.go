package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	return insertUser(ctx, s.db, u, false)
}

func (s *UserStore) CreateFromInvitation(ctx context.Context, u *model.User, invitationID int64) error {
	return s.insertClaimed(ctx, "user create from invitation", u, false, ErrInvitationUnusable, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE invitations SET accepted_at = NOW() WHERE id = $1 AND `+usableInvitationCond,
			invitationID,
		)
		if err != nil {
			return fmt.Errorf("user create from invitation: claim: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("user create from invitation: claim: %w", err)
		} else if n == 0 {
			return ErrInvitationUnusable
		}
		return nil
	})
}

// CreateFromSignupToken inserts u with the email of the link it claims, verified:
// only its owner could have opened the link.
func (s *UserStore) CreateFromSignupToken(ctx context.Context, u *model.User, tokenHash string) error {
	return s.insertClaimed(ctx, "user create from signup token", u, true, ErrSignupTokenUnusable, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx,
			`UPDATE signup_tokens SET used_at = NOW() WHERE token_hash = $1 AND `+usableSignupTokenCond+` RETURNING email`,
			tokenHash,
		).Scan(&u.Email)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSignupTokenUnusable
		}
		if err != nil {
			return fmt.Errorf("user create from signup token: claim: %w", err)
		}
		return nil
	})
}

// insertClaimed runs claim and inserts u in one transaction, so a failed insert
// releases the claim and concurrent submits can't both redeem one link. An email
// registered after the claim is reported as unusable, the rule every claim's
// predicate already applies.
func (s *UserStore) insertClaimed(ctx context.Context, op string, u *model.User, emailVerified bool, unusable error, claim func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback()

	if err := claim(tx); err != nil {
		return err
	}
	if err := insertUser(ctx, tx, u, emailVerified); err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return unusable
		}
		return err
	}
	return tx.Commit()
}

func insertUser(ctx context.Context, db dbtx, u *model.User, emailVerified bool) error {
	err := scanUser(db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, bio, avatar_url, is_invited, email_verified_at)
		 SELECT $1, $2, $3, $4, $5, $6, CASE WHEN $7::boolean THEN NOW() END WHERE NOT `+ownerNameTakenCond+`
		 RETURNING `+userColumns,
		u.Username, u.Email, u.PasswordHash, u.Bio, u.AvatarURL, u.IsInvited, emailVerified,
	), u)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUsernameTaken
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case usersUsernameKey, ownerNameTakenKey:
				return ErrUsernameTaken
			case usersEmailKey, usersEmailLowerKey:
				return ErrEmailTaken
			}
		}
		return fmt.Errorf("user create: %w", err)
	}
	return nil
}

// ownerNameTakenCond holds when $1, in any case, is a username or an org name:
// both are the first URL segment and a directory under the repos root. Inserts
// check it in the same statement; case variants inserted concurrently can still race.
const ownerNameTakenCond = `(EXISTS (SELECT 1 FROM users WHERE lower(username) = lower($1))
	OR EXISTS (SELECT 1 FROM organizations WHERE lower(name) = lower($1)))`

func (s *UserStore) OwnerNameTaken(ctx context.Context, name string) (bool, error) {
	var taken bool
	if err := s.db.QueryRowContext(ctx, `SELECT `+ownerNameTakenCond, name).Scan(&taken); err != nil {
		return false, fmt.Errorf("owner name taken: %w", err)
	}
	return taken, nil
}

func (s *UserStore) CreateOAuthUser(ctx context.Context, username, email, provider, oauthID, avatarURL string, emailVerified bool) (*model.User, error) {
	u := &model.User{}
	err := scanUser(s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, oauth_provider, oauth_id, avatar_url, email_verified_at)
		 SELECT $1, $2, '', $3, $4, $5, CASE WHEN $6::boolean THEN NOW() END WHERE NOT `+ownerNameTakenCond+`
		 RETURNING `+userColumns,
		username, email, provider, oauthID, avatarURL, emailVerified,
	), u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("user create oauth: %w", err)
	}
	return u, nil
}

func (s *UserStore) CreateSuperadmin(ctx context.Context, username, email, passwordHash string) (*model.User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash, bio, avatar_url, is_superadmin, created_at, updated_at)
		 SELECT $1, $2, $3, '', '', TRUE, NOW(), NOW() WHERE NOT `+ownerNameTakenCond,
		username, email, passwordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("create superadmin: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("create superadmin: %w", err)
	} else if n == 0 {
		return nil, ErrUsernameTaken
	}
	return s.GetByEmailWithRole(ctx, email)
}
