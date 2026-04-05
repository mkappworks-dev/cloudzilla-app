package view

import (
	"html/template"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// UserData holds template data for the user profile page.
type UserData struct {
	BasePage
	User           model.User
	Repos          []model.Repository
	RecentActivity []model.Event
	ProfileReadme  template.HTML
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
