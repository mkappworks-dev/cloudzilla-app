package handler

import "github.com/mkappworks/cloudzilla/internal/model"

// Page data structs
type HomeData struct {
	Repos []model.Repository
}

type LoginData struct {
	Error string
}

type UserData struct {
	User  model.User
	Repos []model.Repository
}

type RepoData struct {
	Repo     model.Repository
	Owner    string
	RepoName string
}

type IssuesData struct {
	Repo     model.Repository
	Issues   []model.Issue
	Owner    string
	RepoName string
}

type IssueDetailData struct {
	Repo     model.Repository
	Issue    model.Issue
	Comments []model.Comment
	Owner    string
	RepoName string
}

type PullsData struct {
	Repo     model.Repository
	Pulls    []model.PullRequest
	Owner    string
	RepoName string
}

type PullDetailData struct {
	Repo     model.Repository
	Pull     model.PullRequest
	Owner    string
	RepoName string
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
