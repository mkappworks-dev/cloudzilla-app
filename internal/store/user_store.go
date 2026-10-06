package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	ErrUsernameTaken      = errors.New("username already taken")
	ErrEmailTaken         = errors.New("email already registered")
	ErrOAuthIdentityTaken = errors.New("oauth identity is linked to another account")
)

// The first two are Postgres's default names for the inline UNIQUE columns in
// 001_create_users.sql; users_email_lower_key is the index from 082,
// users_oauth_idx the one from 008, and owner_name_taken is what 080's triggers raise.
const (
	usersUsernameKey   = "users_username_key"
	usersEmailKey      = "users_email_key"
	usersEmailLowerKey = "users_email_lower_key"
	usersOAuthKey      = "users_oauth_idx"
	ownerNameTakenKey  = "owner_name_taken"
)

// UserStore provides database operations for user accounts.
type UserStore struct {
	db *sql.DB
}

// NewUserStore creates a UserStore backed by the given database.
func NewUserStore(database *sql.DB) *UserStore {
	return &UserStore{db: database}
}

// Every query that loads a full model.User selects userColumns and scans with
// scanUser, so a new users column is added in exactly these two places.
const userColumns = `id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
	is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
	notify_pr_review, notify_mention, keep_email_private, email_verified_at, session_version,
	code_theme_light, code_theme_dark, avatar_key`

type rowScanner interface {
	Scan(dest ...any) error
}

// extra receives any columns the query selects after userColumns.
func scanUser(row rowScanner, u *model.User, extra ...any) error {
	dest := []any{&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest,
		&u.NotifyPRReview, &u.NotifyMention, &u.KeepEmailPrivate, &u.EmailVerifiedAt, &u.SessionVersion,
		&u.CodeThemeLight, &u.CodeThemeDark, &u.AvatarKey}
	return row.Scan(append(dest, extra...)...)
}

// filter is the SQL that follows "FROM users".
func (s *UserStore) queryUser(ctx context.Context, filter string, args ...any) (*model.User, error) {
	u := &model.User{}
	if err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users `+filter, args...), u); err != nil {
		return nil, err
	}
	return u, nil
}

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

// notGhost keeps the ghost out of account counts and of lookups by a name or
// address someone typed: it stands in for deleted accounts, so nobody signs in
// as it, finds it, or hands it anything. Lookups by ID still load it for the
// content it holds.
const notGhost = `id <> ghost_user_id()`

func (s *UserStore) GetByID(ctx context.Context, id int64) (*model.User, error) {
	u, err := s.queryUser(ctx, `WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("user get by id: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	u, err := s.queryUser(ctx, `WHERE username = $1 AND `+notGhost, username)
	if err != nil {
		return nil, fmt.Errorf("user get by username: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	u, err := s.queryUser(ctx, `WHERE lower(email) = lower($1) AND `+notGhost, email)
	if err != nil {
		return nil, fmt.Errorf("user get by email: %w", err)
	}
	return u, nil
}

// GetByEmailWithRole fetches a user by email including is_superadmin and is_invited columns.
func (s *UserStore) GetByEmailWithRole(ctx context.Context, email string) (*model.User, error) {
	u, err := s.queryUser(ctx, `WHERE lower(email) = lower($1) AND `+notGhost, email)
	if err != nil {
		return nil, fmt.Errorf("user get by email with role: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByOAuthID(ctx context.Context, provider, oauthID string) (*model.User, error) {
	u, err := s.queryUser(ctx, `WHERE oauth_provider = $1 AND oauth_id = $2 LIMIT 1`, provider, oauthID)
	if err != nil {
		return nil, fmt.Errorf("user get by oauth id: %w", err)
	}
	return u, nil
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

// ClaimReauthAttempt counts an attempt against userID's failure window and
// reports false, counting nothing, once limit attempts in the window are spent.
// The row lock makes concurrent claims take turns, so at most limit get through.
func (s *UserStore) ClaimReauthAttempt(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET
		   reauth_failures = CASE WHEN reauth_window_start IS NULL OR reauth_window_start <= NOW() - make_interval(secs => $3)
		                          THEN 1 ELSE reauth_failures + 1 END,
		   reauth_window_start = CASE WHEN reauth_window_start IS NULL OR reauth_window_start <= NOW() - make_interval(secs => $3)
		                              THEN NOW() ELSE reauth_window_start END
		 WHERE id = $1
		   AND (reauth_window_start IS NULL OR reauth_window_start <= NOW() - make_interval(secs => $3) OR reauth_failures < $2)
		 RETURNING id`,
		userID, limit, window.Seconds(),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("user claim reauth attempt: %w", err)
	}
	return true, nil
}

// ReleaseReauthAttempt uncounts a claimed attempt that succeeded, so only failures use up the window.
func (s *UserStore) ReleaseReauthAttempt(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET reauth_failures = GREATEST(reauth_failures - 1, 0) WHERE id = $1`, userID,
	); err != nil {
		return fmt.Errorf("user release reauth attempt: %w", err)
	}
	return nil
}

// IssueReauthCode stores hash as userID's emailed confirmation code for ttl,
// replacing any earlier one. It reports false, storing nothing, when a code went
// out within gap or perWindow codes went out within window; the checks and the
// write are one statement.
func (s *UserStore) IssueReauthCode(ctx context.Context, userID int64, hash string, ttl, gap time.Duration, perWindow int, window time.Duration) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET reauth_code_hash = $2,
		   reauth_code_expires_at = NOW() + make_interval(secs => $3),
		   reauth_code_sent_at = NOW(),
		   reauth_codes_sent = CASE WHEN reauth_codes_window_start IS NULL OR reauth_codes_window_start <= NOW() - make_interval(secs => $6)
		                            THEN 1 ELSE reauth_codes_sent + 1 END,
		   reauth_codes_window_start = CASE WHEN reauth_codes_window_start IS NULL OR reauth_codes_window_start <= NOW() - make_interval(secs => $6)
		                                    THEN NOW() ELSE reauth_codes_window_start END
		 WHERE id = $1
		   AND (reauth_code_sent_at IS NULL OR reauth_code_sent_at <= NOW() - make_interval(secs => $4))
		   AND (reauth_codes_window_start IS NULL OR reauth_codes_window_start <= NOW() - make_interval(secs => $6) OR reauth_codes_sent < $5)
		 RETURNING id`,
		userID, hash, ttl.Seconds(), gap.Seconds(), perWindow, window.Seconds(),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("user issue reauth code: %w", err)
	}
	return true, nil
}

// StoreReauthCode stores hash as userID's one-time confirmation code for ttl,
// replacing any earlier one. A fresh provider sign-in issues these, so no cap applies.
func (s *UserStore) StoreReauthCode(ctx context.Context, userID int64, hash string, ttl time.Duration) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET reauth_code_hash = $2, reauth_code_expires_at = NOW() + make_interval(secs => $3) WHERE id = $1`,
		userID, hash, ttl.Seconds()); err != nil {
		return fmt.Errorf("user store reauth code: %w", err)
	}
	return nil
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

// LiveReauthCode returns the hash of userID's unexpired emailed code, or ""."
func (s *UserStore) LiveReauthCode(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT CASE WHEN reauth_code_expires_at > NOW() THEN reauth_code_hash ELSE '' END FROM users WHERE id = $1`,
		userID).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("user live reauth code: %w", err)
	}
	return hash, nil
}

// SpendReauthCode uses up the code with hash, reporting false when it was
// already spent, replaced or expired, so a code confirms one action.
func (s *UserStore) SpendReauthCode(ctx context.Context, userID int64, hash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET reauth_code_hash = '', reauth_code_expires_at = NULL
		 WHERE id = $1 AND reauth_code_hash = $2 AND reauth_code_hash <> '' AND reauth_code_expires_at > NOW()`,
		userID, hash)
	if err != nil {
		return false, fmt.Errorf("user spend reauth code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("user spend reauth code rows: %w", err)
	}
	return n == 1, nil
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

func (s *UserStore) CountAccounts(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE `+notGhost).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("user count accounts: %w", err)
	}
	return count, nil
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

// GetByIDWithTOTP fetches a user by ID including TOTP columns.
func (s *UserStore) GetByIDWithTOTP(ctx context.Context, id int64) (*model.User, error) {
	u, err := s.queryUserWithTOTP(ctx, `WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("user get by id with totp: %w", err)
	}
	return u, nil
}

// GetByEmailWithTOTP fetches a user by email including TOTP fields.
func (s *UserStore) GetByEmailWithTOTP(ctx context.Context, email string) (*model.User, error) {
	u, err := s.queryUserWithTOTP(ctx, `WHERE lower(email) = lower($1) AND `+notGhost, email)
	if err != nil {
		return nil, fmt.Errorf("user get by email with totp: %w", err)
	}
	return u, nil
}

func (s *UserStore) queryUserWithTOTP(ctx context.Context, filter string, args ...any) (*model.User, error) {
	u := &model.User{}
	var backupCodesJSON sql.NullString
	// Not totp_backup_codes::text: Postgres quotes array elements only when they need it.
	err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+`, totp_secret, totp_enabled, array_to_json(totp_backup_codes)::text FROM users `+filter, args...),
		u, &u.TOTPSecret, &u.TOTPEnabled, &backupCodesJSON)
	if err != nil {
		return nil, err
	}
	if backupCodesJSON.Valid {
		if err := json.Unmarshal([]byte(backupCodesJSON.String), &u.TOTPBackupCodes); err != nil {
			return nil, fmt.Errorf("decode backup codes: %w", err)
		}
	}
	return u, nil
}

// SetTOTPSecret stores the base32 TOTP secret for a user (does not enable TOTP yet).
func (s *UserStore) SetTOTPSecret(ctx context.Context, userID int64, secret string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_secret = $1, updated_at = NOW() WHERE id = $2`,
		secret, userID,
	)
	if err != nil {
		return fmt.Errorf("user set totp secret: %w", err)
	}
	return nil
}

// SetTOTPEnabled toggles the totp_enabled flag. Pass secret="" to clear it on disable.
func (s *UserStore) SetTOTPEnabled(ctx context.Context, userID int64, enabled bool, secret string) error {
	if enabled {
		_, err := s.db.ExecContext(ctx,
			`UPDATE users SET totp_enabled = TRUE, totp_secret = $1, updated_at = NOW() WHERE id = $2`,
			secret, userID,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_enabled = FALSE, totp_secret = NULL, totp_backup_codes = NULL, updated_at = NOW() WHERE id = $1`,
		userID,
	)
	return err
}

// SetBackupCodes stores bcrypt hashes of backup codes as a PostgreSQL TEXT[].
func (s *UserStore) SetBackupCodes(ctx context.Context, userID int64, codeHashes []string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_backup_codes = $1, updated_at = NOW() WHERE id = $2`,
		codeHashes, userID,
	)
	if err != nil {
		return fmt.Errorf("user set backup codes: %w", err)
	}
	return nil
}

// UpdateProfile reports whether the email changed. A new address starts
// unverified, and links already sent to the old one stop working.
func (s *UserStore) UpdateProfile(ctx context.Context, userID int64, name, email, bio, company, location string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("user update profile begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var oldEmail string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&oldEmail); err != nil {
		return false, fmt.Errorf("user update profile lock: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users
		   SET name=$1, email=$2, bio=$3, company=$4, location=$5, updated_at=NOW(),
		       email_verified_at = CASE WHEN email = $2 THEN email_verified_at END
		 WHERE id=$6`,
		name, email, bio, company, location, userID,
	); err != nil {
		return false, fmt.Errorf("user update profile: %w", err)
	}
	changed := oldEmail != email
	if changed {
		if _, err := tx.ExecContext(ctx,
			`UPDATE email_verification_tokens SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`, userID,
		); err != nil {
			return false, fmt.Errorf("user update profile revoke verification tokens: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("user update profile commit: %w", err)
	}
	return changed, nil
}

// DeleteByID removes a user. Related rows depend on ON DELETE CASCADE in the schema.
func (s *UserStore) DeleteByID(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID)
	if err != nil {
		return fmt.Errorf("user delete: %w", err)
	}
	return nil
}

var ErrOwnedReposChanged = errors.New("user's repositories changed during deletion")

// soleOwnedOrgs selects the orgs user $1 is the only owner of.
const soleOwnedOrgs = `SELECT 1 FROM org_members om
	WHERE om.user_id = $1 AND om.role = 'owner'
	  AND NOT EXISTS (SELECT 1 FROM org_members other
	                  WHERE other.org_id = om.org_id AND other.role = 'owner' AND other.user_id <> $1)`

// IsSoleOrgOwner reports whether deleting userID would leave an org with no owner.
func (s *UserStore) IsSoleOrgOwner(ctx context.Context, userID int64) (bool, error) {
	var sole bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (`+soleOwnedOrgs+`)`, userID).Scan(&sole); err != nil {
		return false, fmt.Errorf("user sole org owner: %w", err)
	}
	return sole, nil
}

// ghostReassignments are the columns that reference users(id) with no ON DELETE
// action; any row left in one would block the user delete. nameCol, when set,
// is the username copied into the row, which would otherwise credit whoever
// registers the freed name next.
var ghostReassignments = []struct{ table, idCol, nameCol string }{
	{"issues", "author_id", ""},
	{"pull_requests", "author_id", ""},
	{"comments", "author_id", "author_name"},
	{"notifications", "actor_id", "actor_name"},
	{"invitations", "invited_by_id", ""},
	{"releases", "author_id", ""},
	{"commit_statuses", "creator_id", ""},
	{"pull_reviews", "author_id", "author_name"},
	{"pull_line_comments", "author_id", "author_name"},
	{"discussions", "author_id", "author_name"},
	{"discussion_replies", "author_id", "author_name"},
	{"pull_events", "actor_id", "actor_name"},
	{"repositories", "deleted_by", ""},
}

// The user's own repos go first, taking everything in them along, so only what
// the user wrote elsewhere passes to the ghost, as on GitHub.
// livePersonalIDs are the repos the caller already moved aside; locking the user
// row blocks new repo inserts (their FK check needs it), so the set is re-checked
// here and a repo created or restored in the meantime aborts the delete. Org
// repos have no owner_id, so neither the cascade nor this check touches them.
// It returns the deleted user's avatar key, read under the row lock.
func (s *UserStore) DeleteWithOwnedRepos(ctx context.Context, userID int64, livePersonalIDs []int64) (avatarKey string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("user delete begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT avatar_key FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&avatarKey); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("user delete lock: %w", err)
	}
	// OrgStore.changeMember takes these locks too, so neither another owner
	// leaving nor this user's promotion can land between this check and the
	// delete. Orgs where the user is only a member count: a promotion would
	// otherwise let their last other owner leave.
	if _, err := tx.ExecContext(ctx,
		`SELECT 1 FROM organizations o JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = $1 ORDER BY o.id FOR NO KEY UPDATE OF o`, userID); err != nil {
		return "", fmt.Errorf("user delete lock orgs: %w", err)
	}
	var sole bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (`+soleOwnedOrgs+`)`, userID).Scan(&sole); err != nil {
		return "", fmt.Errorf("user delete check orgs: %w", err)
	}
	if sole {
		return "", ErrLastOrgOwner
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM repositories WHERE owner_id=$1 AND deleted_at IS NULL FOR UPDATE`, userID)
	if err != nil {
		return "", fmt.Errorf("user delete list repos: %w", err)
	}
	var live []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", fmt.Errorf("user delete scan repo: %w", err)
		}
		live = append(live, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("user delete list repos: %w", err)
	}
	slices.Sort(live)
	expected := slices.Sorted(slices.Values(livePersonalIDs))
	if !slices.Equal(live, expected) {
		return "", ErrOwnedReposChanged
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE owner_id=$1`, userID); err != nil {
		return "", fmt.Errorf("user delete repos: %w", err)
	}
	// Review requests are addressed to the user, and the ghost can never answer one.
	if _, err := tx.ExecContext(ctx, `DELETE FROM pull_reviews WHERE author_id=$1 AND state='pending'`, userID); err != nil {
		return "", fmt.Errorf("user delete review requests: %w", err)
	}
	var ghostID int64
	var ghostName string
	if err := tx.QueryRowContext(ctx, `SELECT id, username FROM users WHERE id = ghost_user_id()`).Scan(&ghostID, &ghostName); err != nil {
		return "", fmt.Errorf("user delete load ghost: %w", err)
	}
	for _, r := range ghostReassignments {
		q := `UPDATE ` + r.table + ` SET ` + r.idCol + ` = $2`
		args := []any{userID, ghostID}
		if r.nameCol != "" {
			q += `, ` + r.nameCol + ` = $3`
			args = append(args, ghostName)
		}
		if _, err := tx.ExecContext(ctx, q+` WHERE `+r.idCol+` = $1`, args...); err != nil {
			return "", fmt.Errorf("user delete reassign %s.%s: %w", r.table, r.idCol, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
		return "", fmt.Errorf("user delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("user delete commit: %w", err)
	}
	return avatarKey, nil
}

func (s *UserStore) UpdateNotificationPrefs(ctx context.Context, userID int64, p model.NotificationPrefs) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users
		   SET email_notifications=$1, email_digest=$2, notify_pr_review=$3, notify_mention=$4, updated_at=NOW()
		 WHERE id=$5`,
		p.EmailNotifications, p.EmailDigest, p.NotifyPRReview, p.NotifyMention, userID,
	)
	if err != nil {
		return fmt.Errorf("user update notification prefs: %w", err)
	}
	return nil
}

func (s *UserStore) UpdateCodeThemes(ctx context.Context, userID int64, light, dark string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET code_theme_light=$1, code_theme_dark=$2, updated_at=NOW() WHERE id=$3`,
		light, dark, userID,
	)
	if err != nil {
		return fmt.Errorf("user update code themes: %w", err)
	}
	return nil
}

// GetLayoutPrefs reads what every page's layout needs about its viewer.
func (s *UserStore) GetLayoutPrefs(ctx context.Context, userID int64) (light, dark, avatarKey string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT code_theme_light, code_theme_dark, avatar_key FROM users WHERE id=$1`, userID,
	).Scan(&light, &dark, &avatarKey)
	if err != nil {
		return "", "", "", fmt.Errorf("user get layout prefs: %w", err)
	}
	return light, dark, avatarKey, nil
}

func (s *UserStore) UpdateKeepEmailPrivate(ctx context.Context, userID int64, keep bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET keep_email_private=$1, updated_at=NOW() WHERE id=$2`,
		keep, userID,
	)
	if err != nil {
		return fmt.Errorf("user update keep email private: %w", err)
	}
	return nil
}

// ListUsersForDigest returns users who have email notifications enabled with the given digest mode.
func (s *UserStore) ListUsersForDigest(ctx context.Context, digestMode string) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email_notifications = TRUE AND email_digest = $1`,
		digestMode,
	)
	if err != nil {
		return nil, fmt.Errorf("list users for digest: %w", err)
	}
	defer rows.Close()
	return scanFullUsers(rows)
}

// Returns only the rows that exist; ordering is undefined.
func (s *UserStore) GetManyByUsernames(ctx context.Context, usernames []string) ([]model.User, error) {
	if len(usernames) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(usernames))
	args := make([]any, len(usernames))
	for i, u := range usernames {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = u
	}
	q := `SELECT ` + userColumns + ` FROM users WHERE username IN (` + strings.Join(placeholders, ",") + `) AND ` + notGhost
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("user get many by usernames: %w", err)
	}
	defer rows.Close()
	return scanFullUsers(rows)
}

// UsernamesByIDs maps id→username; missing IDs are absent.
func (s *UserStore) UsernamesByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := `SELECT id, username FROM users WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("user usernames by ids: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var username string
		if err := rows.Scan(&id, &username); err != nil {
			return nil, err
		}
		out[id] = username
	}
	return out, rows.Err()
}

// Returns only the rows that exist; ordering is undefined.
func (s *UserStore) GetManyByIDs(ctx context.Context, ids []int64) ([]model.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := `SELECT ` + userColumns + ` FROM users WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("user get many by ids: %w", err)
	}
	defer rows.Close()
	return scanFullUsers(rows)
}

func scanFullUsers(rows *sql.Rows) ([]model.User, error) {
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := scanUser(rows, &u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func formatPGInt64Array(ids []int64) string {
	if len(ids) == 0 {
		return "{}"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func parsePGInt64Array(s string) ([]int64, error) {
	s = strings.Trim(s, "{}")
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// AddPinnedRepo drops the prune IDs and appends repoID under a row lock, so
// overlapping pins can't overwrite each other. It reports false, changing
// nothing, when the pins left after pruning already number limit.
func (s *UserStore) AddPinnedRepo(ctx context.Context, userID, repoID int64, prune []int64, limit int) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("user add pinned repo: begin: %w", err)
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRowContext(ctx,
		`SELECT pinned_repo_ids::text FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&raw); err != nil {
		return false, fmt.Errorf("user add pinned repo: lock: %w", err)
	}
	ids, err := parsePGInt64Array(raw)
	if err != nil {
		return false, fmt.Errorf("user add pinned repo: parse: %w", err)
	}
	if slices.Contains(ids, repoID) {
		return true, nil
	}
	ids = slices.DeleteFunc(ids, func(id int64) bool { return slices.Contains(prune, id) })
	if len(ids) >= limit {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET pinned_repo_ids = $2::bigint[], updated_at = NOW() WHERE id = $1`,
		userID, formatPGInt64Array(append(ids, repoID)),
	); err != nil {
		return false, fmt.Errorf("user add pinned repo: update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("user add pinned repo: commit: %w", err)
	}
	return true, nil
}

func (s *UserStore) RemovePinnedRepo(ctx context.Context, userID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET pinned_repo_ids = array_remove(pinned_repo_ids, $2::bigint), updated_at = NOW()
		 WHERE id = $1 AND $2::bigint = ANY(pinned_repo_ids)`,
		userID, repoID,
	)
	if err != nil {
		return fmt.Errorf("user remove pinned repo: %w", err)
	}
	return nil
}

func (s *UserStore) GetPinnedRepoIDs(ctx context.Context, userID int64) ([]int64, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT pinned_repo_ids::text FROM users WHERE id = $1`, userID,
	).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("user get pinned repo ids: %w", err)
	}
	return parsePGInt64Array(raw)
}

// SwapAvatarKey sets the user's avatar key and returns the one it replaced,
// read under the row lock so two concurrent swaps each see the other's key.
// It returns sql.ErrNoRows when the user doesn't exist.
func (s *UserStore) SwapAvatarKey(ctx context.Context, userID int64, key string) (string, error) {
	var old string
	err := s.db.QueryRowContext(ctx,
		`UPDATE users u SET avatar_key = $2, updated_at = NOW()
		   FROM (SELECT id, avatar_key FROM users WHERE id = $1 FOR UPDATE) prev
		  WHERE u.id = prev.id
		 RETURNING prev.avatar_key`, userID, key,
	).Scan(&old)
	if err != nil {
		return "", fmt.Errorf("user swap avatar key: %w", err)
	}
	return old, nil
}

// AvatarKeysByOwnerName maps each user or org name that has an avatar to its
// key. Users and orgs share one namespace, so a name matches at most one row.
func (s *UserStore) AvatarKeysByOwnerName(ctx context.Context, names []string) (map[string]string, error) {
	keys := map[string]string{}
	if len(names) == 0 {
		return keys, nil
	}
	placeholders := make([]string, len(names))
	args := make([]any, len(names))
	for i, name := range names {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = name
	}
	in := strings.Join(placeholders, ",")
	rows, err := s.db.QueryContext(ctx,
		`SELECT username, avatar_key FROM users WHERE avatar_key <> '' AND username IN (`+in+`)
		 UNION ALL
		 SELECT name, avatar_key FROM organizations WHERE avatar_key <> '' AND name IN (`+in+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("avatar keys by owner name: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, key string
		if err := rows.Scan(&name, &key); err != nil {
			return nil, fmt.Errorf("avatar keys by owner name scan: %w", err)
		}
		keys[name] = key
	}
	return keys, rows.Err()
}
