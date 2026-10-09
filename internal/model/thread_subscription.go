package model

import "time"

const (
	ThreadKindIssue      = "issue"
	ThreadKindPull       = "pull"
	ThreadKindDiscussion = "discussion"
)

const (
	ThreadStateSubscribed = "subscribed"
	ThreadStateMuted      = "muted"
)

const (
	ThreadReasonAuthor  = "author"
	ThreadReasonComment = "comment"
	ThreadReasonReview  = "review"
	ThreadReasonMention = "mention"
	ThreadReasonAssign  = "assign"
	ThreadReasonManual  = "manual"

	// ThreadReasonWatching marks a status derived from a repo watch; it is never stored.
	ThreadReasonWatching = "watching"
)

// ThreadSubscription is a user's subscription to, or mute of, one issue, pull request or discussion.
type ThreadSubscription struct {
	UserID    int64     `db:"user_id"    json:"user_id"`
	RepoID    int64     `db:"repo_id"    json:"repo_id"`
	Kind      string    `db:"kind"       json:"kind"`
	Number    int64     `db:"number"     json:"number"`
	State     string    `db:"state"      json:"state"`
	Reason    string    `db:"reason"     json:"reason"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}
