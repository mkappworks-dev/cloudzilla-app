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
	User           model.User
	Repos          []model.Repository
	RecentActivity []model.Event
	ProfileReadme  template.HTML
	IsOwnProfile   bool
	Tab            string // "overview" | "repositories"
	PinnedRepos    []components.PinnedRepoData
	Heatmap        map[time.Time]int
	TopLangs       []components.LangBarItem
	Orgs           []service.OrgMembership
}

// OrgData holds template data for the organization profile page.
type OrgData struct {
	BasePage
	Org       model.Organization
	Repos     []model.Repository
	Members   []model.OrgMember
	CanManage bool
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
	Org     model.Organization
	Members []model.OrgMember
}

// OrgMembersFragData holds template data for the org members HTMX fragment.
type OrgMembersFragData struct {
	OrgName   string
	Members   []model.OrgMember
	CanManage bool
}

// NewOrganizationData holds template data for the new-organization form page.
type NewOrganizationData struct {
	BasePage
	Error       string // non-empty re-renders the form with an error banner
	Name        string // preserved on validation-error re-render
	Description string // preserved on re-render
}

// UserStarsData holds template data for the user's starred repositories page.
type UserStarsData struct {
	BasePage
	ProfileUser model.User
	Repos       []model.Repository
}

// UserGistsData holds template data for the user's gists list page.
type UserGistsData struct {
	BasePage
	ProfileUser model.User
	Gists       []model.Gist
	Page        int
}
