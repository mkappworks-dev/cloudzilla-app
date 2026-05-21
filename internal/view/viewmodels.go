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
	TotalRepos   int // authoritative count from DB (may exceed len(Repos) after future pagination)
	RepoOpenPRs  map[int64]int // open PR count per repo ID; missing key → 0
	RepoSort     string        // "updated" | "name" | "created"
	RepoFilter   string        // "all" | "sources" | "forks" | "templates"
	Templates    []model.Repository
	Stats        []components.StatItem
	Heatmap      map[time.Time]int
	Attention    []service.AttentionItem
	Activity     []model.Event
	LoadWarnings []string
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
	Pulls         []store.PullListItem
	Filter        string // "created" | "assigned" | "review_requested" | "mentioned"
	State         string // "open" | "closed"
	PullComments  map[int64]int
	PullLabels    map[int64][]model.Label
	PullCI        map[int64]service.CIChecks
	PullReviewers map[int64][]model.PullReview
}

type AccountIssuesData struct {
	BasePage
	Issues []store.IssueListItem
	Filter string // "assigned" | "created" | "mentioned"
	State  string // "open" | "closed"
}

type AccountStarsData struct {
	BasePage
	Username  string
	Stars     []model.Repository
	Language  string   // active language chip (empty == all)
	Languages []string // distinct primary_language values for chip rendering
}
