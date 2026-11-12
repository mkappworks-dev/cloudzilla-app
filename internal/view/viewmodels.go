package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
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
	// Original is the head-branch line a pending suggestion would replace; HasOriginal is false when it couldn't be read.
	Original    string
	HasOriginal bool
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
	// OwnerContext is the user or org whose page is being viewed (e.g. `/{owner}`
	// or `/{owner}/{repo}`). The topbar's workspace switcher labels its trigger
	// with this name when set, falling back to the logged-in user otherwise.
	OwnerContext string
	// SetupPending hides site-wide links: until setup completes, every route but /setup redirects back to it.
	SetupPending bool
	// CodeLight and CodeDark are the viewer's code themes; read them through CodeThemeLight/CodeThemeDark.
	CodeLight string
	CodeDark  string
	// AvatarKey is the viewer's avatar, for the nav menu.
	AvatarKey string
	AdminMode *AdminModeInfo
}

type AdminModeInfo struct {
	ExpiresAt time.Time
}

// ISO and Clock are in UTC because the server doesn't know the viewer's zone.
func (i AdminModeInfo) ISO() string   { return i.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z") }
func (i AdminModeInfo) Clock() string { return i.ExpiresAt.UTC().Format("15:04") }

// CodeThemeLight falls back to the default for pages built without basePage(), such as setup and error pages.
func (b BasePage) CodeThemeLight() string {
	if b.CodeLight == "" {
		return highlight.DefaultLight
	}
	return b.CodeLight
}

func (b BasePage) CodeThemeDark() string {
	if b.CodeDark == "" {
		return highlight.DefaultDark
	}
	return b.CodeDark
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
	Active string // "overview" | "repositories" | "gists" | "explore-gists" | "pulls" | "issues"
	// Username is the signed-in user; the Gists tab leads to their profile's Gists tab.
	Username string
	Counts   map[string]int
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
	HeatmapYear    int                     // calendar year shown in the commit heatmap
	HeatmapTotal   int                     // commit total for HeatmapYear
	HeatmapYears   []int                   // selectable years, most recent first
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
	LoadFailed  bool           // feed query errored — render an error state, not the empty state
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
	Filter       string         // "all" | "owned" | "collaborator" | "forks"
	TypeCounts   map[string]int // repo count per type tab
	Language     string         // selected language filter; "" = all
	Languages    []string
	Sort         string // "updated" | "name" | "stars" | "created"
	StarCounts   map[int64]int
	CommitCounts map[int64]int
	Topics       map[int64][]model.Topic
	UserID       int64
	Total        int // repos owned or collaborated on, before filtering
	Page         int
	TotalPages   int

	IncomingTransfers int
}

// AccountListTab is one scope tab of an account list: a link that sets a single @me qualifier.
type AccountListTab struct {
	Label    string
	URL      string
	Active   bool
	CountKey string // key into Counts, "<scope>:<state>"
}

// AccountListNav is what the account issue and pull request lists share around their rows.
type AccountListNav struct {
	Filter      components.ListFilterData
	ScopeTabs   []AccountListTab
	StateFilter string
	OpenURL     string
	ClosedURL   string
	// ActiveScope is the Counts scope of the one active tab, or empty when none is, in which case the state tabs show no
	// count.
	ActiveScope string
	Counts      map[string]int // keyed "<scope>:<state>"
	ClearURL    string
	Page        int
	TotalPages  int
	PrevURL     string
	NextURL     string
}

type AccountPullsData struct {
	BasePage
	AccountListNav
	Pulls         []store.PullListItem
	PullComments  map[int64]int
	PullLabels    map[int64][]model.Label
	PullCI        map[int64]service.CIChecks
	PullReviewers map[int64][]model.PullReview
}

type AccountIssuesData struct {
	BasePage
	AccountListNav
	Issues        []store.IssueListItem
	IssueComments map[int64]int
	IssueLabels   map[int64][]model.Label
}

type AccountStarsData struct {
	BasePage
	Username  string
	Stars     []model.Repository
	Language  string   // active language chip (empty == all)
	Languages []string // distinct primary_language values for chip rendering
}
