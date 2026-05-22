package model

import (
	"database/sql"
	"time"
)

type User struct {
	ID              int64          `db:"id"               json:"id"`
	Username        string         `db:"username"         json:"username"`
	Email           string         `db:"email"            json:"email"`
	PasswordHash    string         `db:"password_hash"    json:"-"`
	Name            string         `db:"name"             json:"name"`
	Bio             string         `db:"bio"              json:"bio"`
	Company         string         `db:"company"          json:"company"`
	Location        string         `db:"location"         json:"location"`
	AvatarURL       string         `db:"avatar_url"       json:"avatar_url"`
	OAuthProvider   string         `db:"oauth_provider"   json:"-"`
	OAuthID         string         `db:"oauth_id"         json:"-"`
	IsSuperadmin    bool           `db:"is_superadmin"    json:"-"`
	IsInvited       bool           `db:"is_invited"       json:"-"`
	TOTPSecret      sql.NullString `db:"totp_secret"      json:"-"`
	TOTPEnabled     bool           `db:"totp_enabled"     json:"-"`
	TOTPBackupCodes []string       `db:"-"                json:"-"`
	CreatedAt          time.Time      `db:"created_at"          json:"created_at"`
	UpdatedAt          time.Time      `db:"updated_at"          json:"updated_at"`
	EmailNotifications  bool   `db:"email_notifications"   json:"email_notifications"`
	EmailDigest         string `db:"email_digest"          json:"email_digest"`
	NotifyPRReview      bool   `db:"notify_pr_review"      json:"notify_pr_review"`
	NotifyIssueAssigned bool   `db:"notify_issue_assigned" json:"notify_issue_assigned"`
	NotifyMention       bool   `db:"notify_mention"        json:"notify_mention"`
	NotifyWatched       bool   `db:"notify_watched"        json:"notify_watched"`
	NotifyWeeklyDigest  bool   `db:"notify_weekly_digest"  json:"notify_weekly_digest"`
}
