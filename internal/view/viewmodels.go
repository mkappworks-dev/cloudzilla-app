package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

type RenderedComment struct {
	model.Comment
	BodyHTML string
}

type RenderedLineComment struct {
	model.PullLineComment
	BodyHTML string
}

type BasePage struct {
	CurrentUser       *middleware.Claims
	UnreadNotifCount  int
	AllowLogin        bool
	AllowRegistration bool
	UserOrgs          []OrgEntry
	RepoSubnav        *RepoSubnavInfo
	AccountSubnav     *AccountSubnavInfo
	RepoSwitcher      []RepoRef
}

type OrgEntry struct {
	Org  model.Organization
	Role model.OrgRole
}

type RepoSubnavInfo struct {
	OwnerName string
	RepoName  string
	Active    string
	Counts    map[string]int
	CanManage bool
	Private   bool
	// Feature toggles — when false the corresponding tab is hidden.
	AllowIssues      bool
	AllowDiscussions bool
	AllowProjects    bool
	AllowWiki        bool
}

type RepoRef struct {
	Name string
	Path string // "/{owner}/{repo}"
}

// A missing or zero Counts entry hides that tab's badge.
type AccountSubnavInfo struct {
	Active string // "overview" | "repositories" | "gists" | "pulls" | "issues"
	Counts map[string]int
}

type HomeData struct {
	BasePage
	Repos        []model.Repository
	Templates    []model.Repository
	Stats        []components.StatItem
	Heatmap      map[time.Time]int
	Attention    []service.AttentionItem
	Activity     []model.Event
	LoadWarnings []string
}

type FeedData struct {
	BasePage
	Events      []model.Event
	Page        int
	HasNextPage bool
}

type ActivityData struct {
	BasePage
	Username string
	Events   []model.Event
	Page     int
	HasMore  bool
}

type AccountReposData struct {
	BasePage
	Repos  []model.Repository
	Filter string // "all" | "owned" | "collaborator"
}

type AccountPullsData struct {
	BasePage
	Pulls  []store.PullListItem
	Filter string // "created" | "assigned" | "review_requested" | "mentioned"
	State  string // "open" | "closed"
}

type AccountIssuesData struct {
	BasePage
	Issues []store.IssueListItem
	Filter string // "assigned" | "created" | "mentioned"
	State  string // "open" | "closed"
}
