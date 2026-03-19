package handler

import (
	"html/template"

	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

// RenderedComment wraps a model.Comment with its body pre-rendered as HTML.
type RenderedComment struct {
	model.Comment
	BodyHTML template.HTML
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
	Repo       model.Repository
	Owner      string
	RepoName   string
	CloneHTTP  string
	CloneSSH   string
	CanWrite   bool
	ReadmeHTML template.HTML
}

type RepoSettingsData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Webhooks    []model.Webhook
	Collabs     []model.Permission
	CanManage   bool
	CanTransfer bool
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
	Comments []RenderedComment
	Owner    string
	RepoName string
	BodyHTML template.HTML
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
	BodyHTML template.HTML
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
	Issue    model.Issue
	Owner    string
	Repo     string
	BodyHTML template.HTML
}

type PullDetailFragData struct {
	Pull     model.PullRequest
	Owner    string
	Repo     string
	BodyHTML template.HTML
}

type CommentFragData struct {
	Comment RenderedComment
}

type CommentsFragData struct {
	Comments []RenderedComment
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

type SetupData struct {
	BasePage
	Error string
}

type AdminSettingsData struct {
	BasePage
	Settings    []model.SiteSetting
	Invitations []model.Invitation
}

type AdminSettingsFragData struct {
	Settings []model.SiteSetting
}

type AdminInvitationsFragData struct {
	Invitations []model.Invitation
}

type InviteData struct {
	BasePage
	Invitation *model.Invitation
	Error      string
}

type RepoCollaboratorsFragData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Collabs  []model.Permission
	CanWrite bool
}
