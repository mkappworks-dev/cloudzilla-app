package model

import "time"

const (
	EventPush             = "push"
	EventComment          = "comment"
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

// CommitSummary is one commit shown inside a push activity event.
type CommitSummary struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// PushSummary describes the commits pushed to a single branch.
type PushSummary struct {
	Branch      string          `json:"branch"`
	CommitTotal int             `json:"commit_total"`
	Commits     []CommitSummary `json:"commits"`
}

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
