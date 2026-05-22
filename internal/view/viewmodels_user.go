package view

import (
	"html/template"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// UserData holds template data for the user profile page.
type UserData struct {
	BasePage
	User           model.User
	Repos          []model.Repository
	RecentActivity []model.Event
	ProfileReadme  template.HTML

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
}

// OrgData holds template data for the organization profile page.
type OrgData struct {
	BasePage
	Org       model.Organization
	Repos     []model.Repository
	Members   []model.OrgMember
	CanManage bool
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
