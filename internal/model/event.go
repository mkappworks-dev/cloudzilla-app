package model

import "time"

const (
	EventPush             = "push"
	EventIssueOpened      = "issue_opened"
	EventIssueClosed      = "issue_closed"
	EventPROpened         = "pr_opened"
	EventPRMerged         = "pr_merged"
	EventPRClosed         = "pr_closed"
	EventFork             = "fork"
	EventStar             = "star"
	EventReleasePublished = "release_published"
	EventMemberAdded      = "member_added"
)

type Event struct {
	ID        int64     `db:"id"         json:"id"`
	ActorID   int64     `db:"actor_id"   json:"actor_id"`
	ActorName string    `db:"actor_name" json:"actor_name"`
	RepoID    *int64    `db:"repo_id"    json:"repo_id,omitempty"`
	RepoName  string    `db:"repo_name"  json:"repo_name"`
	OwnerName string    `db:"owner_name" json:"owner_name"`
	EventType string    `db:"event_type" json:"event_type"`
	Payload   []byte    `db:"payload"    json:"payload"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
