package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// RenderedComment wraps a model.Comment with its body pre-rendered as HTML.
// RenderedComment holds a comment body pre-rendered to safe HTML.
type RenderedComment struct {
	model.Comment
	BodyHTML string
}

// RenderedLineComment wraps PullLineComment with pre-rendered HTML body.
// RenderedLineComment holds an inline diff comment pre-rendered to safe HTML.
type RenderedLineComment struct {
	model.PullLineComment
	BodyHTML string
}

// BasePage contains common data for all pages
// BasePage holds data common to every rendered page, including the current user and unread notification count.
type BasePage struct {
	CurrentUser       *middleware.Claims
	UnreadNotifCount  int
	AllowLogin        bool
	AllowRegistration bool
	// UserOrgs is the list of organizations the current user owns.
	// Empty if no user is signed in. Used by the layout's workspace switcher.
	UserOrgs []model.Organization
}

// HomeData holds template data for the home dashboard page.
// Stats/Heatmap/Attention/Activity are only populated for authenticated viewers.
type HomeData struct {
	BasePage
	Repos     []model.Repository
	Templates []model.Repository
	Stats     []components.StatItem
	Heatmap   map[time.Time]int
	Attention []service.AttentionItem
	Activity  []model.Event
}

// Feed page
// FeedData holds template data for the activity feed page.
type FeedData struct {
	BasePage
	Events      []model.Event
	Page        int
	HasNextPage bool
}
