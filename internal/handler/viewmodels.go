package handler

// This file re-exports all view-model types from internal/view so that handler
// code can continue to reference them without a package qualifier.  The
// canonical definitions now live in internal/view/viewmodels.go.

import "github.com/mkappworks/cloudzilla/internal/view"

type (
	BasePage               = view.BasePage
	RenderedComment        = view.RenderedComment
	RenderedLineComment    = view.RenderedLineComment
	HomeData               = view.HomeData
	FeedData               = view.FeedData
	LoginData              = view.LoginData
	UserData               = view.UserData
	OrgData                = view.OrgData
	OrgSettingsData        = view.OrgSettingsData
	OrgMembersFragData     = view.OrgMembersFragData
	RepoData               = view.RepoData
	ReleasesData           = view.ReleasesData
	ReleaseDetailData      = view.ReleaseDetailData
	RepoSettingsData       = view.RepoSettingsData
	BranchProtectionsFragData = view.BranchProtectionsFragData
	DeployKeysFragData     = view.DeployKeysFragData
	WebhooksFragData       = view.WebhooksFragData
	IssuesData             = view.IssuesData
	IssueDetailData        = view.IssueDetailData
	PullsData              = view.PullsData
	PullDetailData         = view.PullDetailData
	IssueNewData           = view.IssueNewData
	PullNewData            = view.PullNewData
	PRReviewsFragData      = view.PRReviewsFragData
	SettingsData           = view.SettingsData
	NotificationsData      = view.NotificationsData
	NotificationsFragData  = view.NotificationsFragData
	NotificationItemFragData = view.NotificationItemFragData
	IssueDetailFragData    = view.IssueDetailFragData
	PullDetailFragData     = view.PullDetailFragData
	CommentFragData        = view.CommentFragData
	CommentsFragData       = view.CommentsFragData
	RefsData               = view.RefsData
	BranchesFragData       = view.BranchesFragData
	TagsFragData           = view.TagsFragData
	TreeData               = view.TreeData
	BlobData               = view.BlobData
	BlameData              = view.BlameData
	CommitsData            = view.CommitsData
	CommitData             = view.CommitData
	SetupData              = view.SetupData
	AdminSettingsData      = view.AdminSettingsData
	AdminSettingsFragData  = view.AdminSettingsFragData
	AdminInvitationsFragData = view.AdminInvitationsFragData
	InviteData             = view.InviteData
	RepoCollaboratorsFragData = view.RepoCollaboratorsFragData
	IssueLabelSidebarData  = view.IssueLabelSidebarData
	PullLabelSidebarData   = view.PullLabelSidebarData
	IssueAssigneeSidebarData = view.IssueAssigneeSidebarData
	PullAssigneeSidebarData  = view.PullAssigneeSidebarData
	RepoLabelsFragData     = view.RepoLabelsFragData
	ForkButtonData         = view.ForkButtonData
	StarButtonData         = view.StarButtonData
	MilestonesData         = view.MilestonesData
	MilestonesListFragData = view.MilestonesListFragData
	MilestoneSidebarFragData = view.MilestoneSidebarFragData
	LineCommentsFragData   = view.LineCommentsFragData
	LineCommentFormFragData = view.LineCommentFormFragData
	SearchData             = view.SearchData
	StargazersData         = view.StargazersData
	UserStarsData          = view.UserStarsData
	TokensData             = view.TokensData
	TokensListFragData     = view.TokensListFragData
	ReactionFragData       = view.ReactionFragData
	SSHKeysFragData        = view.SSHKeysFragData
	AuditLogData           = view.AuditLogData
	ProjectsData           = view.ProjectsData
	ProjectDetailData      = view.ProjectDetailData
	SSOSettingsData        = view.SSOSettingsData
	PulseData              = view.PulseData
	ContributorsData       = view.ContributorsData
	OAuthAuthorizeData     = view.OAuthAuthorizeData
	OAuthAppsData          = view.OAuthAppsData
)
