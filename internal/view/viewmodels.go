package view

import (
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
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
	Repo          model.Repository
	Owner         string
	RepoName      string
	CloneHTTP     string
	CloneSSH      string
	CanWrite      bool
	ReadmeHTML    string
	StarCount     int
	IsStarred     bool
	ForkCount     int
	IsFork        bool
	ForkOfPath    string
	LatestRelease *model.Release
}

// Releases page
type ReleasesData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Releases []model.Release
	CanWrite bool
}

// Release detail page
type ReleaseDetailData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Release  model.Release
	BodyHTML string
	CanWrite bool
}

type RepoSettingsData struct {
	BasePage
	Repo              model.Repository
	Owner             string
	RepoName          string
	Webhooks          []model.Webhook
	Collabs           []model.Permission
	Labels            []model.Label
	DeployKeys        []model.DeployKey
	BranchProtections []*model.BranchProtection
	CanManage         bool
	CanTransfer       bool
}

type BranchProtectionsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Rules     []*model.BranchProtection
	CanManage bool
}

type DeployKeysFragData struct {
	Owner      string
	RepoName   string
	RepoID     int64
	DeployKeys []model.DeployKey
	CanManage  bool
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
	Repo            model.Repository
	Issues          []model.Issue
	Owner           string
	RepoName        string
	IssueLabels     map[int64][]model.Label
	AllMilestones   []model.Milestone
	ActiveMilestone *model.Milestone
}

type IssueDetailData struct {
	BasePage
	Repo          model.Repository
	Issue         model.Issue
	Comments      []RenderedComment
	Owner         string
	RepoName      string
	BodyHTML      string
	Labels        []model.Label
	Assignees     []model.User
	AllLabels     []model.Label
	Milestone     *model.Milestone
	AllMilestones []model.Milestone
	CanWrite      bool
}

type PullsData struct {
	BasePage
	Repo            model.Repository
	Pulls           []model.PullRequest
	Owner           string
	RepoName        string
	PullLabels      map[int64][]model.Label
	AllMilestones   []model.Milestone
	ActiveMilestone *model.Milestone
	StateFilter     string
}

type PullDetailData struct {
	BasePage
	Repo              model.Repository
	Pull              model.PullRequest
	Owner             string
	RepoName          string
	Diff              *service.PRDiffResult
	BodyHTML          string
	Labels            []model.Label
	Assignees         []model.User
	AllLabels         []model.Label
	Milestone         *model.Milestone
	AllMilestones     []model.Milestone
	CanWrite          bool
	HeadStatuses      []model.CommitStatus
	Reviews           []model.PullReview
	CanMerge          bool
	MergeBlockReason  string
	AutoMergeEnabled  bool
	AutoMergeStrategy string
	// LineComments keyed by "path:line" (e.g. "src/main.go:42")
	LineComments map[string][]RenderedLineComment
}

// IssueNewData is used by the new-issue page (template chooser + form).
type IssueNewData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	Templates []service.IssueTemplate
	// Selected is the pre-filled body when a specific template was chosen.
	Selected string
	// ShowForm is true when the form should be shown (vs. the template chooser).
	ShowForm bool
	Error    string
}

// PullNewData is used by the new-pull-request page.
type PullNewData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	TemplateBody string
	Branches     []service.BranchInfo
	Error        string
}

type PRReviewsFragData struct {
	Owner            string
	RepoName         string
	PullNumber       int
	Reviews          []model.PullReview
	CanWrite         bool
	PullOpen         bool
	CanMerge         bool
	MergeBlockReason string
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
	BodyHTML string
}

type PullDetailFragData struct {
	Pull              model.PullRequest
	Owner             string
	Repo              string
	BodyHTML          string
	CanWrite          bool
	AutoMergeEnabled  bool
	AutoMergeStrategy string
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
	Statuses []model.CommitStatus
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

// Label sidebar fragments
type IssueLabelSidebarData struct {
	Owner       string
	RepoName    string
	IssueNumber int
	Labels      []model.Label
	AllLabels   []model.Label
	CanWrite    bool
}

type PullLabelSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Labels     []model.Label
	AllLabels  []model.Label
	CanWrite   bool
}

// Assignee sidebar fragments
type IssueAssigneeSidebarData struct {
	Owner       string
	RepoName    string
	IssueNumber int
	Assignees   []model.User
	CanWrite    bool
}

type PullAssigneeSidebarData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Assignees  []model.User
	CanWrite   bool
}

// Repo labels management (settings page)
type RepoLabelsFragData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Labels   []model.Label
	CanWrite bool
}

// Fork button fragment
type ForkButtonData struct {
	BasePage
	Owner     string
	RepoName  string
	ForkCount int
}

// Star button fragment
type StarButtonData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Count     int
	IsStarred bool
	LoggedIn  bool
}

// Milestones page
type MilestonesData struct {
	BasePage
	Repo             model.Repository
	Owner            string
	RepoName         string
	OpenMilestones   []model.Milestone
	ClosedMilestones []model.Milestone
	CanWrite         bool
}

// Milestones list fragment (HTMX swap)
type MilestonesListFragData struct {
	Owner            string
	RepoName         string
	OpenMilestones   []model.Milestone
	ClosedMilestones []model.Milestone
	CanWrite         bool
}

// Milestone sidebar fragment for issue/PR detail pages
type MilestoneSidebarFragData struct {
	Owner         string
	RepoName      string
	ItemNumber    int
	IsPull        bool
	Current       *model.Milestone
	AllMilestones []model.Milestone
	CanWrite      bool
}

// Line comment fragments
type LineCommentsFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
	Comments   []RenderedLineComment
	CanWrite   bool
}

type LineCommentFormFragData struct {
	Owner      string
	RepoName   string
	PullNumber int
	Path       string
	Line       int
}

// Search page
type SearchData struct {
	BasePage
	Query   string
	Type    string
	Results *service.SearchResults
}

// Stargazers page
type StargazersData struct {
	BasePage
	Repo       model.Repository
	Owner      string
	RepoName   string
	Stargazers []model.User
	StarCount  int
}

// User starred repos page
type UserStarsData struct {
	BasePage
	ProfileUser model.User
	Repos       []model.Repository
}

// Tokens page
type TokensData struct {
	BasePage
	Tokens   []model.AccessToken
	NewToken string // raw token, shown only once after creation
}

// Tokens list fragment
type TokensListFragData struct {
	Tokens []model.AccessToken
}

// ReactionFragData is used by the reactions fragment.
type ReactionFragData struct {
	Owner     string
	RepoName  string
	CommentID int64
	Reactions []model.ReactionSummary
	LoggedIn  bool
}

// SSHKeysFragData is used by the SSH keys fragment.
type SSHKeysFragData struct {
	SSHKeys []model.SSHKey
}

// SecurityPageData is the view model for GET /settings/security.
type SecurityPageData struct {
	BasePage
	TOTPEnabled bool
	TOTPSecret  string   // pending secret, shown only before first verification
	OTPAuthURL  string   // otpauth:// URL for QR code (shown only when setting up)
	BackupCodes []string // raw backup codes, shown only once after enable
	Error       string
	Success     string
}

// TOTPVerifyPageData is the view model for GET /auth/2fa.
type TOTPVerifyPageData struct {
	BasePage
	Error string
}

// AuditLogData holds data for the admin audit log page.
type AuditLogData struct {
	BasePage
	Entries    []model.AuditEntry
	Filter     model.AuditFilter
	TotalCount int
	Page       int
	PerPage    int
}
