package model

import "time"

// Repository represents a git repository and its metadata.
type Repository struct {
	ID            int64      `db:"id"             json:"id"`
	OwnerID       int64      `db:"owner_id"       json:"owner_id"`
	OwnerName     string     `db:"owner_name"     json:"owner_name,omitempty"`
	OrgID         int64      `db:"org_id"         json:"org_id,omitempty"`
	Name          string     `db:"name"           json:"name"`
	Description   string     `db:"description"    json:"description"`
	Website       string     `db:"website"        json:"website,omitempty"`
	License       string     `db:"license"        json:"license,omitempty"`
	AllowIssues      bool    `db:"allow_issues"      json:"allow_issues"`
	AllowDiscussions bool    `db:"allow_discussions" json:"allow_discussions"`
	AllowProjects    bool    `db:"allow_projects"    json:"allow_projects"`
	AllowWiki        bool    `db:"allow_wiki"        json:"allow_wiki"`
	Private       bool       `db:"private"        json:"private"`
	DefaultBranch string     `db:"default_branch" json:"default_branch"`
	CreatedAt     time.Time  `db:"created_at"     json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"     json:"updated_at"`
	IsFork        bool       `db:"is_fork"        json:"is_fork"`
	ForkOfID      *int64     `db:"fork_of_id"     json:"fork_of_id,omitempty"`
	ForkOfOwner   string     `db:"-"              json:"fork_of_owner,omitempty"`
	ForkOfName    string     `db:"-"              json:"fork_of_name,omitempty"`
	ForkCount     int        `db:"fork_count"     json:"fork_count"`
	IsArchived    bool       `db:"is_archived"    json:"is_archived"`
	ArchivedAt    *time.Time `db:"archived_at"    json:"archived_at,omitempty"`
	IsTemplate    bool       `db:"is_template"    json:"is_template"`
	DeletedAt     *time.Time `db:"deleted_at"     json:"deleted_at,omitempty"`
	DeletedBy     *int64     `db:"deleted_by"     json:"deleted_by,omitempty"`
}

// RepositoryWithStats augments a Repository with aggregated star and fork counts
// for display on explore/trending pages.
// RepositoryWithStats extends Repository with aggregated issue, PR, and star counts.
type RepositoryWithStats struct {
	Repository
	StarCount int `db:"star_count"`
}
