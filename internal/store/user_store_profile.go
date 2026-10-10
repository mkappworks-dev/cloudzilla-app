package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

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
