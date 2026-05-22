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
	Repos          []model.Repository
	TotalRepos     int           // authoritative count from DB (may exceed len(Repos) after future pagination)
	RepoOpenPRs    map[int64]int // open PR count per repo ID; missing key → 0
	RepoSort       string        // "updated" | "name" | "created"
	RepoFilter     string        // "all" | "sources" | "forks" | "templates"
	Templates      []model.Repository
	Stats          []components.StatItem
	Heatmap        map[time.Time]int
	HeatmapYear    int   // calendar year shown in the commit heatmap
	HeatmapTotal   int   // commit total for HeatmapYear
	HeatmapYears   []int // selectable years, most recent first
	Attention      []service.AttentionItem // top 3 preview items
	AttentionTotal int                     // total across all kinds
	Activity       []model.Event
	LoadWarnings   []string
}

type AttentionData struct {
	BasePage
	Items  []service.AttentionItem
	Kind   string         // active filter: "all" | "mentions" | "reviews" | "assigned"
	Sort   string         // "overdue" | "newest" | "oldest"
	Counts map[string]int // keys: "all", "mentions", "reviews", "assigned"
	Total  int
}

type ActivityData struct {
	BasePage
	Username    string
	Events      []model.Event
	Filter      string         // "all" | "yours" | "watching"
	ScopeCounts map[string]int // total events per scope, keyed "all"|"yours"|"watching"
	Page        int
	HasMore     bool
	PrevURL     string
	NextURL     string
}

type AccountReposData struct {
	BasePage
	Repos        []model.Repository
	Filter       string // "all" | "owned" | "collaborator" | "forks"
	TypeCounts   map[string]int // repo count per type tab
	Language     string // selected language filter; "" = all
	Languages    []string
	Sort         string // "updated" | "name" | "stars" | "created"
	StarCounts   map[int64]int
	CommitCounts map[int64]int
	Topics       map[int64][]model.Topic
	UserID       int64
	Total        int // repos owned or collaborated on, before filtering
	Page         int
	TotalPages   int
}

type AccountPullsData struct {
	BasePage
	Pulls         []store.PullListItem
	Filter        string         // "created" | "assigned" | "review_requested" | "mentioned"
	State         string         // "open" | "closed"
	Sort          string         // "newest" | "oldest" | "updated" | "comments"
	Counts        map[string]int // PR count per tab, keyed "filter:state"
	PullComments  map[int64]int
	PullLabels    map[int64][]model.Label
	PullCI        map[int64]service.CIChecks
	PullReviewers map[int64][]model.PullReview
}

type AccountIssuesData struct {
	BasePage
	Issues        []store.IssueListItem
	Filter        string // "assigned" | "created" | "mentioned"
	State         string // "open" | "closed"
	Sort          string // "newest" | "oldest" | "updated" | "comments"
	IssueComments map[int64]int
	IssueLabels   map[int64][]model.Label
	Counts        map[string]int // keyed "<filter>:<state>", e.g. "assigned:open"
}

type AccountStarsData struct {
	BasePage
	Username  string
	Stars     []model.Repository
	Language  string   // active language chip (empty == all)
	Languages []string // distinct primary_language values for chip rendering
}
