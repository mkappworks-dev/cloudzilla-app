package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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
	UserOrgs          []model.Organization
	RepoSubnav        *RepoSubnavInfo
}

type RepoSubnavInfo struct {
	OwnerName string
	RepoName  string
	Active    string
	Counts    map[string]int
	CanManage bool
	// Feature toggles — when false the corresponding tab is hidden.
	AllowIssues      bool
	AllowDiscussions bool
	AllowProjects    bool
	AllowWiki        bool
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
