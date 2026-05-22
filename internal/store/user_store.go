package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// UserStore provides database operations for user accounts.
type UserStore struct {
	db *sql.DB
}

// NewUserStore creates a UserStore backed by the given database.
func NewUserStore(database *sql.DB) *UserStore {
	return &UserStore{db: database}
}

func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, bio, avatar_url)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at, updated_at`,
		u.Username, u.Email, u.PasswordHash, u.Bio, u.AvatarURL,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("user create: %w", err)
	}
	return nil
}

func (s *UserStore) GetByID(ctx context.Context, id int64) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by id: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by username: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by email: %w", err)
	}
	return u, nil
}

// GetByEmailWithRole fetches a user by email including is_superadmin and is_invited columns.
func (s *UserStore) GetByEmailWithRole(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by email with role: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByOAuthID(ctx context.Context, provider, oauthID string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE oauth_provider = $1 AND oauth_id = $2 LIMIT 1`,
		provider, oauthID,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("user get by oauth id: %w", err)
	}
	return u, nil
}

func (s *UserStore) LinkOAuth(ctx context.Context, userID int64, provider, oauthID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET oauth_provider = $1, oauth_id = $2, updated_at = NOW() WHERE id = $3`,
		provider, oauthID, userID,
	)
	if err != nil {
		return fmt.Errorf("user link oauth: %w", err)
	}
	return nil
}

func (s *UserStore) CreateOAuthUser(ctx context.Context, username, email, provider, oauthID, avatarURL string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, oauth_provider, oauth_id, avatar_url)
		 VALUES ($1, $2, '', $3, $4, $5)
		 RETURNING id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		           is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest`,
		username, email, provider, oauthID, avatarURL,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("user create oauth: %w", err)
	}
	return u, nil
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
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, totp_secret, totp_enabled,
		        totp_backup_codes::text,
		        created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE id = $1`,
		id,
	).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.TOTPSecret, &u.TOTPEnabled, &backupCodesStr,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest,
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
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, totp_secret, totp_enabled,
		        totp_backup_codes::text,
		        created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE email = $1`,
		email,
	).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.TOTPSecret, &u.TOTPEnabled, &backupCodesStr,
		&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest,
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

// UpdateProfile saves the user's profile fields: name, username, email, bio, company, location.
// Returns an error if the new username or email collides at the DB level.
func (s *UserStore) UpdateProfile(ctx context.Context, userID int64, name, username, email, bio, company, location string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users
		   SET name=$1, username=$2, email=$3, bio=$4, company=$5, location=$6, updated_at=NOW()
		 WHERE id=$7`,
		name, username, email, bio, company, location, userID,
	)
	if err != nil {
		return fmt.Errorf("user update profile: %w", err)
	}
	return nil
}

// DeleteByID removes a user. Related rows depend on ON DELETE CASCADE in the schema.
func (s *UserStore) DeleteByID(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID)
	if err != nil {
		return fmt.Errorf("user delete: %w", err)
	}
	return nil
}

// NotificationPrefs is the set of granular email notification toggles
// surfaced on /settings#notifications.
type NotificationPrefs struct {
	PRReview      bool
	IssueAssigned bool
	Mention       bool
	Watched       bool
	WeeklyDigest  bool
}

// UpdateNotificationPrefs saves the user's granular notification toggles.
func (s *UserStore) UpdateNotificationPrefs(ctx context.Context, userID int64, p NotificationPrefs) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users
		   SET notify_pr_review=$1, notify_issue_assigned=$2, notify_mention=$3,
		       notify_watched=$4, notify_weekly_digest=$5, updated_at=NOW()
		 WHERE id=$6`,
		p.PRReview, p.IssueAssigned, p.Mention, p.Watched, p.WeeklyDigest, userID,
	)
	if err != nil {
		return fmt.Errorf("user update notification prefs: %w", err)
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
		`SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
		 FROM users WHERE email_notifications = TRUE AND email_digest = $1`,
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
	q := `SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
	             is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
	      FROM users WHERE username IN (` + strings.Join(placeholders, ",") + `)`
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
	q := `SELECT id, username, email, password_hash, name, bio, company, location, avatar_url, oauth_provider, oauth_id,
	             is_superadmin, is_invited, created_at, updated_at, email_notifications, email_digest,
		        notify_pr_review, notify_issue_assigned, notify_mention, notify_watched, notify_weekly_digest
	      FROM users WHERE id IN (` + strings.Join(placeholders, ",") + `)`
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
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Name, &u.Bio, &u.Company, &u.Location, &u.AvatarURL,
			&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
			&u.CreatedAt, &u.UpdatedAt, &u.EmailNotifications, &u.EmailDigest, &u.NotifyPRReview, &u.NotifyIssueAssigned, &u.NotifyMention, &u.NotifyWatched, &u.NotifyWeeklyDigest); err != nil {
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

// formatPGInt64Array formats []int64 as a Postgres int-array literal like "{1,2,3}".
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

// parsePGInt64Array parses a Postgres int-array literal "{1,2,3}" into []int64.
// Returns nil for "{}" or empty strings.
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

// SetPinnedRepoIDs replaces the user's pinned repo IDs with the given slice,
// preserving order. Pass an empty slice to clear.
func (s *UserStore) SetPinnedRepoIDs(ctx context.Context, userID int64, ids []int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET pinned_repo_ids = $2::bigint[], updated_at = NOW() WHERE id = $1`,
		userID, formatPGInt64Array(ids),
	)
	if err != nil {
		return fmt.Errorf("user set pinned repo ids: %w", err)
	}
	return nil
}

// GetPinnedRepoIDs returns the user's pinned repo IDs in pin order.
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
