package view

import (
	"html/template"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type UserData struct {
	BasePage
	User           model.User
	Repos          []model.Repository
	RecentActivity []model.Event
	ProfileReadme  template.HTML
}

type OrgData struct {
	BasePage
	Org       model.Organization
	Repos     []model.Repository
	Members   []model.OrgMember
	CanManage bool
}

type OrgSettingsData struct {
	BasePage
	Org     model.Organization
	Members []model.OrgMember
}

type OrgMembersFragData struct {
	OrgName   string
	Members   []model.OrgMember
	CanManage bool
}

type UserStarsData struct {
	BasePage
	ProfileUser model.User
	Repos       []model.Repository
}

type UserGistsData struct {
	BasePage
	ProfileUser model.User
	Gists       []model.Gist
	Page        int
}
