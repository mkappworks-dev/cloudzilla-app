package handler

import (
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

// BasePage contains common data for all pages
type BasePage struct {
	CurrentUser      *middleware.Claims
	UnreadNotifCount int
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

type RepoData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	CloneHTTP string
	CloneSSH  string
	CanWrite  bool
}

type RepoSettingsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Webhooks []model.Webhook
}

type WebhooksFragData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Webhooks []model.Webhook
	CanWrite bool
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
	Diff     *service.PRDiffResult
}

type SettingsData struct {
	BasePage
	SSHKeys []model.SSHKey
}

type NotificationsData struct {
	BasePage
	Notifications []model.Notification
	UnreadCount   int
}

type NotificationsFragData struct {
	Notifications []model.Notification
}

type NotificationItemFragData struct {
	Notification model.Notification
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

type RefsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Branches []service.BranchInfo
	Tags     []service.TagInfo
	CanWrite bool
}

type BranchesFragData struct {
	Owner         string
	RepoName      string
	Branches      []service.BranchInfo
	CanWrite      bool
	DefaultBranch string
}

type TagsFragData struct {
	Owner    string
	RepoName string
	Tags     []service.TagInfo
	CanWrite bool
}

type TreeData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Ref         string
	Path        string
	Breadcrumbs []service.BreadcrumbPart
	Entries     []service.TreeEntry
	RefsURL     string
}

type BlobData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Ref         string
	Path        string
	Breadcrumbs []service.BreadcrumbPart
	Lines       []service.CodeLine
	IsBinary    bool
	BlameURL    string
}

type BlameData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Ref         string
	Path        string
	Breadcrumbs []service.BreadcrumbPart
	Lines       []service.BlameLine
	BlobURL     string
}

type CommitsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Log      *service.CommitLog
	RefsURL  string
}

type CommitData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Commit   *service.CommitDetail
}
