package view

import (
	"html/template"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// UserData holds template data for the user profile page.
type UserData struct {
	BasePage
	User             model.User
	Repos            []model.Repository
	RecentActivity   []model.Event
	ProfileReadme    template.HTML
	ProfileReadmeRaw string
	ReadmeError      string

	IsOwnProfile bool
	Tab          string // "overview" | "repositories" | "stars" | "gists"
	PinnedRepos  []components.PinnedRepoData
	Heatmap      map[time.Time]int
	TopLangs     []components.LangBarItem
	Orgs         []service.OrgMembership

	// HasProfileRepo is true when the user owns a public repo named after
	// themselves; the README itself may still be empty.
	HasProfileRepo           bool
	ProfileRepoDefaultBranch string

	PinnedRepoIDs map[int64]bool

	// Tab nav counters (always populated)
	ReposTotal int
	StarsTotal int
	GistsTotal int

	// Repositories tab — populated only when ?tab=repositories
	RepoTabRepos          []model.Repository
	RepoTabRoles          map[int64]string        // viewer's role per repo ID
	RepoTabLanguages      []string                // distinct primary languages for filter chips
	RepoTabStars          map[int64]int           // star count per repo ID
	RepoTabTopics         map[int64][]model.Topic // up to 3 topics per repo ID
	RepoTabActiveQuery    string
	RepoTabActiveType     string // "sources" | "forks" | "templates" | ""
	RepoTabActiveLanguage string
	RepoTabActiveStatus   string // "public" | "private" | ""
	RepoTabPage           int
	RepoTabTotalPages     int

	// Stars tab — populated only when ?tab=stars
	StarredRepos       []model.Repository
	StarsTabPage       int
	StarsTabTotalPages int

	// Gists tab — populated only when ?tab=gists
	GistsTabItems      []GistTabItem
	GistsTabPage       int
	GistsTabTotalPages int
}

// GistTabItem is one gist row on the profile Gists tab, with its derived language label.
type GistTabItem struct {
	model.Gist
	FileCount     int
	LanguageLabel string
	LanguageClass string
}

// OrgData holds template data for the organization profile page.
type OrgData struct {
	BasePage
	Org           model.Organization
	Repos         []model.Repository
	Members       []model.OrgMember
	MemberCount   int
	CanManage     bool
	ProfileReadme template.HTML
	PinnedRepos   []components.PinnedRepoData
	RecentRepos   []components.PinnedRepoData
	ShowAllRepos  bool // ?tab=repositories: RecentRepos holds every visible repo
	ShowAllPeople bool // ?tab=people
	TopLangs      []components.LangBarItem
	ViewerRole    *model.OrgRole // nil if the viewer is not a member
	ViewerJoined  *time.Time     // nil if the viewer is not a member
}

// OrgListData holds template data for the organizations listing page.
type OrgListData struct {
	BasePage
	Entries []OrgListEntry
}

// OrgListEntry is one organization row with the viewer's role and member count.
type OrgListEntry struct {
	Org         model.Organization
	Role        model.OrgRole
	MemberCount int
}

// OrgSettingsData holds template data for the organization settings page.
type OrgSettingsData struct {
	BasePage
	Org          model.Organization
	Members      []model.OrgMember
	MemberCount  int
	RepoCount    int
	AuditEntries []model.AuditEntry
}

// OrgMembersFragData holds template data for the org members HTMX fragment.
type OrgMembersFragData struct {
	OrgName   string
	Members   []model.OrgMember
	CanManage bool
	ViewerID  int64 // for marking the viewer's own row with a "You" badge
}

// NewOrganizationData holds template data for the new-organization form page.
type NewOrganizationData struct {
	BasePage
	Error       string // non-empty re-renders the form with an error banner
	Name        string // preserved on validation-error re-render
	Description string // preserved on re-render
}
