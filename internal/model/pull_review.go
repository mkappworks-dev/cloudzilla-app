package model

import "time"

// PRReviewState represents the state of a pull request review.
type PRReviewState string

const (
	PRReviewApproved         PRReviewState = "approved"
	PRReviewChangesRequested PRReviewState = "changes_requested"
	PRReviewCommented        PRReviewState = "commented"
	PRReviewPending          PRReviewState = "pending"
)

// PullReview represents a reviewer's submitted review on a pull request.
type PullReview struct {
	ID          int64         `db:"id"           json:"id"`
	PullID      int64         `db:"pull_id"      json:"pull_id"`
	RepoID      int64         `db:"repo_id"      json:"repo_id"`
	AuthorID    int64         `db:"author_id"    json:"author_id"`
	AuthorName  string        `db:"author_name"  json:"author_name"`
	State       PRReviewState `db:"state"        json:"state"`
	Body        string        `db:"body"         json:"body"`
	SubmittedAt *time.Time    `db:"submitted_at" json:"submitted_at,omitempty"`
	CreatedAt   time.Time     `db:"created_at"   json:"created_at"`
	UpdatedAt   time.Time     `db:"updated_at"   json:"updated_at"`
}
