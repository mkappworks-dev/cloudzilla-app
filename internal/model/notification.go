package model

import "time"

type NotificationType string

const (
	NotifIssueComment  NotificationType = "issue_comment"
	NotifPRComment     NotificationType = "pr_comment"
	NotifIssueClosed   NotificationType = "issue_closed"
	NotifIssueReopened NotificationType = "issue_reopened"
	NotifPRMerged      NotificationType = "pr_merged"
	NotifPRClosed      NotificationType = "pr_closed"
	NotifPROpened      NotificationType = "pr_opened"
	NotifPRReview      NotificationType = "pr_review"
	NotifMention            NotificationType = "mention"
	NotifDiscussionReply    NotificationType = "discussion_reply"
)

type Notification struct {
	ID         int64            `db:"id"          json:"id"`
	UserID     int64            `db:"user_id"     json:"user_id"`
	ActorID    int64            `db:"actor_id"    json:"actor_id"`
	ActorName  string           `db:"actor_name"  json:"actor_name"`
	Type       NotificationType `db:"type"        json:"type"`
	RepoID     int64            `db:"repo_id"     json:"repo_id"`
	RepoName   string           `db:"repo_name"   json:"repo_name"`
	OwnerName  string           `db:"owner_name"  json:"owner_name"`
	SubjectID  int64            `db:"subject_id"  json:"subject_id"`
	SubjectURL string           `db:"subject_url" json:"subject_url"`
	Read       bool             `db:"read"        json:"read"`
	CreatedAt  time.Time        `db:"created_at"  json:"created_at"`
}
