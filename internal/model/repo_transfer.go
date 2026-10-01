package model

import "time"

// RepoTransfer is a repository offered to another user. It moves only once
// they accept; the owner and name fields are the repository's current ones.
type RepoTransfer struct {
	ID            int64     `db:"id"           json:"id"`
	RepoID        int64     `db:"repo_id"      json:"repo_id"`
	OwnerName     string    `json:"owner"`
	RepoName      string    `json:"repo"`
	Description   string    `json:"description"`
	Private       bool      `json:"private"`
	RequesterID   int64     `db:"requester_id" json:"requester_id"`
	RequesterName string    `json:"requester"`
	RecipientID   int64     `db:"recipient_id" json:"recipient_id"`
	RecipientName string    `json:"recipient"`
	ExpiresAt     time.Time `db:"expires_at"   json:"expires_at"`
	CreatedAt     time.Time `db:"created_at"   json:"created_at"`
}

// FullName is owner/repo, what the recipient sees and confirms when accepting.
func (t RepoTransfer) FullName() string {
	return t.OwnerName + "/" + t.RepoName
}
