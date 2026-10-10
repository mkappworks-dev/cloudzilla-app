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
	code_theme_light, code_theme_dark, suspended_at, avatar_key`

type rowScanner interface {
	Scan(dest ...any) error
}

// extra receives any columns the query selects after userColumns.
func scanUser(row rowScanner, u *model.User, extra ...any) error {
	dest := []any{&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest,
		&u.NotifyPRReview, &u.NotifyMention, &u.KeepEmailPrivate, &u.EmailVerifiedAt, &u.SessionVersion,
		&u.CodeThemeLight, &u.CodeThemeDark, &u.SuspendedAt, &u.AvatarKey}
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

func (s *UserStore) CountAccounts(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE `+notGhost).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("user count accounts: %w", err)
	}
	return count, nil
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
