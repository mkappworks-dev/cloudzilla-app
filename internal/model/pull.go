package model

import (
	"fmt"
	"time"
)

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

type PullRequest struct {
	ID                int64      `db:"id"            json:"id"`
	RepoID            int64      `db:"repo_id"       json:"repo_id"`
	Number            int        `db:"number"        json:"number"`
	AuthorID          int64      `db:"author_id"     json:"author_id"`
	AuthorName        string     `db:"author_name"   json:"author_name,omitempty"`
	Title             string     `db:"title"         json:"title"`
	Body              string     `db:"body"          json:"body"`
	State             PRState    `db:"state"         json:"state"`
	HeadBranch        string     `db:"head_branch"   json:"head_branch"`
	BaseBranch        string     `db:"base_branch"   json:"base_branch"`
	MilestoneID       *int64     `db:"milestone_id"  json:"milestone_id,omitempty"`
	CreatedAt         time.Time  `db:"created_at"    json:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"    json:"updated_at"`
	MergedAt          *time.Time `db:"merged_at"     json:"merged_at"`
	ClosedAt          *time.Time `db:"closed_at"     json:"closed_at"`
	IsDraft           bool       `db:"is_draft"             json:"is_draft"`
	DraftAt           *time.Time `db:"draft_at"             json:"draft_at,omitempty"`
	AutoMergeEnabled  bool       `db:"auto_merge_enabled"   json:"auto_merge_enabled"`
	AutoMergeStrategy string     `db:"auto_merge_strategy"  json:"auto_merge_strategy,omitempty"`
	HeadSHA           string     `db:"head_sha"             json:"head_sha,omitempty"`
	// HeadRepoID is the repository the head branch lives in; nil once that repository is gone.
	HeadRepoID *int64 `db:"head_repo_id" json:"-"`
	// CrossRepo is set by GetByID, GetByNumber and Create when the head branch is not in the base repository.
	CrossRepo bool `db:"-" json:"-"`
	// HeadRepo is the head's "owner/name" when it differs from the base, set by GetByID, GetByNumber and Create only.
	HeadRepo string `db:"-" json:"head_repo,omitempty"`
	// AutoMergeBy is the user who armed auto-merge, read by GetByID and GetByNumber only.
	AutoMergeBy *int64 `db:"auto_merge_by" json:"-"`
	// MergedBaseSHA and MergedHeadSHA are the base tip and head tip the merge saw, read by GetByID and GetByNumber
	// only. Both are empty for a PR merged before they were recorded and not rebuilt since.
	MergedBaseSHA string `db:"-" json:"-"`
	MergedHeadSHA string `db:"-" json:"-"`

	// Set only by queries that can return another repo's pull requests.
	RepoOwner string `db:"-" json:"repo_owner,omitempty"`
	RepoName  string `db:"-" json:"repo_name,omitempty"`
}

// HeadRef names the head in the base repository: the branch, or for a pull request from another repository the ref
// that holds a copy of its head commit.
func (p *PullRequest) HeadRef() string {
	if p.CrossRepo {
		return fmt.Sprintf("refs/pull/%d/head", p.Number)
	}
	return p.HeadBranch
}
