package view

import (
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
)

// RenderedComment wraps a model.Comment with its body pre-rendered as HTML.
type RenderedComment struct {
	model.Comment
	BodyHTML string
}

// RenderedLineComment wraps PullLineComment with pre-rendered HTML body.
type RenderedLineComment struct {
	model.PullLineComment
	BodyHTML string
}

// BasePage contains common data for all pages
type BasePage struct {
	CurrentUser      *middleware.Claims
	UnreadNotifCount int
	AllowLogin       bool
}

// Page data structs
type HomeData struct {
	BasePage
	Repos     []model.Repository
	Templates []model.Repository
}

// Feed page
type FeedData struct {
	BasePage
	Events      []model.Event
	Page        int
	HasNextPage bool
}
