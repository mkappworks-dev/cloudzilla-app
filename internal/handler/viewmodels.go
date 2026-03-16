package handler

import (
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
)

// BasePage contains common data for all pages
type BasePage struct {
	CurrentUser *middleware.Claims
}

// Page data structs
type HomeData struct {
	BasePage
	Repos []model.Repository
}

type LoginData struct {
	BasePage
	Error string
}

type UserData struct {
	BasePage
	User  model.User
	Repos []model.Repository
}

type RepoData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	CloneHTTP string
	CloneSSH  string
}

type IssuesData struct {
	BasePage
	Repo     model.Repository
	Issues   []model.Issue
	Owner    string
	RepoName string
}

type IssueDetailData struct {
	BasePage
	Repo     model.Repository
	Issue    model.Issue
	Comments []model.Comment
	Owner    string
	RepoName string
}

type PullsData struct {
	BasePage
	Repo     model.Repository
	Pulls    []model.PullRequest
	Owner    string
	RepoName string
}

type PullDetailData struct {
	BasePage
	Repo     model.Repository
	Pull     model.PullRequest
	Owner    string
	RepoName string
}

type SettingsData struct {
	BasePage
	SSHKeys []model.SSHKey
}

// Fragment data structs
type IssueDetailFragData struct {
	Issue model.Issue
	Owner string
	Repo  string
}

type PullDetailFragData struct {
	Pull  model.PullRequest
	Owner string
	Repo  string
}

type CommentFragData struct {
	Comment model.Comment
}

type CommentsFragData struct {
	Comments []model.Comment
}
