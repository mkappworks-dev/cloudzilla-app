package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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
	CurrentUser      *middleware.Claims
	UnreadNotifCount int
	AllowLogin       bool
}

// Page data structs
// HomeData holds template data for the home feed page.
type HomeData struct {
	BasePage
	Repos     []model.Repository
	Templates []model.Repository
}

// Feed page
// FeedData holds template data for the activity feed page.
type FeedData struct {
	BasePage
	Events      []model.Event
	Page        int
	HasNextPage bool
}
