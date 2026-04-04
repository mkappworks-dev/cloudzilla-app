package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
	storedb "github.com/mkappworks/cloudzilla/internal/store/db"
)

type UserStore struct {
	q  *storedb.Queries
	db *sql.DB
}

func NewUserStore(q *storedb.Queries, database *sql.DB) *UserStore {
	return &UserStore{q: q, db: database}
}


func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	result, err := s.q.CreateUser(ctx, storedb.CreateUserParams{
		Username:     u.Username,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Bio:          u.Bio,
		AvatarUrl:    u.AvatarURL,
	})
	if err != nil {
		return fmt.Errorf("user create: %w", err)
	}
	u.ID = result.ID
	u.CreatedAt = result.CreatedAt
	u.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *UserStore) GetByID(ctx context.Context, id int64) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by id: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	result, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user get by username: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	result, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user get by email: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

// GetByEmailWithRole fetches a user by email including is_superadmin and is_invited columns
// added via ALTER TABLE (not known to sqlc).
func (s *UserStore) GetByEmailWithRole(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by email with role: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByOAuthID(ctx context.Context, provider, oauthID string) (*model.User, error) {
	result, err := s.q.GetUserByOAuthID(ctx, provider, oauthID)
	if err != nil {
		return nil, fmt.Errorf("user get by oauth id: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) LinkOAuth(ctx context.Context, userID int64, provider, oauthID string) error {
	if err := s.q.LinkOAuth(ctx, userID, provider, oauthID); err != nil {
		return fmt.Errorf("user link oauth: %w", err)
	}
	return nil
}

func (s *UserStore) CreateOAuthUser(ctx context.Context, username, email, provider, oauthID, avatarURL string) (*model.User, error) {
	result, err := s.q.CreateOAuthUser(ctx, storedb.CreateOAuthUserParams{
		Username:      username,
		Email:         email,
		OAuthProvider: provider,
		OAuthID:       oauthID,
		AvatarURL:     avatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("user create oauth: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) CountAll(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("user count all: %w", err)
	}
	return count, nil
}

func (s *UserStore) CreateSuperadmin(ctx context.Context, username, email, passwordHash string) (*model.User, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash, bio, avatar_url, is_superadmin, created_at, updated_at)
		 VALUES ($1, $2, $3, '', '', TRUE, NOW(), NOW())`,
		username, email, passwordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("create superadmin: %w", err)
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

func (s *UserStore) MarkInvited(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET is_invited = TRUE WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("user mark invited: %w", err)
	}
	return nil
}

// GetByIDWithTOTP fetches a user by ID including TOTP columns.
func (s *UserStore) GetByIDWithTOTP(ctx context.Context, id int64) (*model.User, error) {
	u := &model.User{}
	var backupCodesStr sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, totp_secret, totp_enabled,
		        totp_backup_codes::text,
		        created_at, updated_at, email_notifications, email_digest
		 FROM users WHERE id = $1`,
		id,
	).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.TOTPSecret, &u.TOTPEnabled, &backupCodesStr,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest,
	)
	if err != nil {
		return nil, fmt.Errorf("user get by id with totp: %w", err)
	}
	if backupCodesStr.Valid && backupCodesStr.String != "" {
		jsonBytes := postgresArrayToJSON(backupCodesStr.String)
		_ = json.Unmarshal(jsonBytes, &u.TOTPBackupCodes)
	}
	return u, nil
}

// GetByEmailWithTOTP fetches a user by email including TOTP fields.
func (s *UserStore) GetByEmailWithTOTP(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	var backupCodesStr sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, totp_secret, totp_enabled,
		        totp_backup_codes::text,
		        created_at, updated_at, email_notifications, email_digest
		 FROM users WHERE email = $1`,
		email,
	).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.TOTPSecret, &u.TOTPEnabled, &backupCodesStr,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest,
	)
	if err != nil {
		return nil, fmt.Errorf("user get by email with totp: %w", err)
	}
	if backupCodesStr.Valid && backupCodesStr.String != "" {
		jsonBytes := postgresArrayToJSON(backupCodesStr.String)
		_ = json.Unmarshal(jsonBytes, &u.TOTPBackupCodes)
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
	raw, err := json.Marshal(codeHashes)
	if err != nil {
		return fmt.Errorf("marshal backup codes: %w", err)
	}
	pgArr := jsonToPostgresArray(raw)
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET totp_backup_codes = $1, updated_at = NOW() WHERE id = $2`,
		pgArr, userID,
	)
	if err != nil {
		return fmt.Errorf("user set backup codes: %w", err)
	}
	return nil
}

// UpdateEmailPrefs saves the user's email notification preferences.
func (s *UserStore) UpdateEmailPrefs(ctx context.Context, userID int64, emailNotifications bool, emailDigest string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET email_notifications=$1, email_digest=$2, updated_at=NOW() WHERE id=$3`,
		emailNotifications, emailDigest, userID,
	)
	return err
}

// ListUsersForDigest returns users who have email notifications enabled with the given digest mode.
func (s *UserStore) ListUsersForDigest(ctx context.Context, digestMode string) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest
		 FROM users WHERE email_notifications = TRUE AND email_digest = $1`,
		digestMode,
	)
	if err != nil {
		return nil, fmt.Errorf("list users for digest: %w", err)
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
			&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited, &u.CreatedAt, &u.UpdatedAt,
			&u.EmailNotifications, &u.EmailDigest); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// jsonToPostgresArray converts a JSON array like ["a","b"] to PostgreSQL literal {"a","b"}.
func jsonToPostgresArray(jsonArr []byte) string {
	var items []string
	if err := json.Unmarshal(jsonArr, &items); err != nil {
		return "{}"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		escaped := strings.ReplaceAll(item, `"`, `\"`)
		quoted[i] = `"` + escaped + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

// postgresArrayToJSON converts a PostgreSQL array literal {"a","b"} to JSON ["a","b"].
func postgresArrayToJSON(pgArr string) []byte {
	pgArr = strings.TrimSpace(pgArr)
	if len(pgArr) < 2 || pgArr[0] != '{' || pgArr[len(pgArr)-1] != '}' {
		return []byte("[]")
	}
	inner := pgArr[1 : len(pgArr)-1]
	if inner == "" {
		return []byte("[]")
	}
	return []byte("[" + inner + "]")
}

func mapDBUserToModel(dbUser *storedb.User) *model.User {
	return &model.User{
		ID:            dbUser.ID,
		Username:      dbUser.Username,
		Email:         dbUser.Email,
		PasswordHash:  dbUser.PasswordHash,
		Bio:           dbUser.Bio,
		AvatarURL:     dbUser.AvatarUrl,
		OAuthProvider: dbUser.OauthProvider,
		OAuthID:       dbUser.OauthID,
		CreatedAt:     dbUser.CreatedAt,
		UpdatedAt:     dbUser.UpdatedAt,
	}
}
