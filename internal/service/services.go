package service

import (
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type Services struct {
	User         *UserService
	Repo         *RepoService
	Issue        *IssueService
	Pull         *PullService
	Comment      *CommentService
	SSHKey       *SSHKeyService
	Code         *CodeService
	Org          *OrgService
	Webhook      *WebhookService
	Notification *NotificationService
	SiteSetting  *SiteSettingService
	Invitation   *InvitationService
	Label        *LabelService
	Assignee     *AssigneeService
	Star         *StarService
	Release      *ReleaseService
	CommitStatus *CommitStatusService
	Milestone    *MilestoneService
	PullReview      *PullReviewService
	PullLineComment *PullLineCommentService
	Search          *SearchService
	AccessToken      *AccessTokenService
	DeployKey        *DeployKeyService
	BranchProtection *BranchProtectionService
	Reaction         *ReactionService
	TOTP             *TOTPService
	AuditLog         *AuditService
	SSO              *SSOService
}

func New(stores *store.Stores, cfg *config.Config) *Services {
	code := NewCodeService(cfg.Git)
	siteSettingSvc := NewSiteSettingService(stores.SiteSetting, stores.User)
	return &Services{
		User:         NewUserService(stores.User, cfg.Auth),
		Repo:         NewRepoService(stores.Repo, stores.User, stores.Org, cfg.Git),
		Issue:        NewIssueService(stores.Issue, stores.Repo),
		Pull:         NewPullService(stores.Pull, stores.Repo),
		Comment:      NewCommentService(stores.Comment),
		SSHKey:       NewSSHKeyService(stores.SSHKey, stores.User),
		Code:         code,
		Org:          NewOrgService(stores.Org, stores.Repo, stores.User, cfg.Git),
		Webhook:      NewWebhookService(stores.Webhook),
		Notification: NewNotificationService(stores.Notification),
		SiteSetting:  siteSettingSvc,
		Invitation:   NewInvitationService(stores.Invitation),
		Label:        NewLabelService(stores.Label, stores.Repo, stores.Issue, stores.Pull),
		Assignee:     NewAssigneeService(stores.Assignee, stores.Repo, stores.Issue, stores.Pull, stores.User),
		Star:         NewStarService(stores.Star, stores.Repo, stores.User),
		Release:      NewReleaseService(stores.Release, stores.Repo, code),
		CommitStatus: NewCommitStatusService(stores.CommitStatus, stores.Repo),
		Milestone:    NewMilestoneService(stores.Milestone, stores.Repo),
		PullReview:      NewPullReviewService(stores.PullReview, stores.Pull, stores.Repo),
		PullLineComment: NewPullLineCommentService(stores.PullLineComment, stores.Pull, stores.Repo),
		Search:          NewSearchService(stores.Search),
		AccessToken:      NewAccessTokenService(stores.AccessToken, stores.User),
		DeployKey:        NewDeployKeyService(stores.DeployKey, stores.SSHKey),
		BranchProtection: NewBranchProtectionService(stores.BranchProtection, stores.PullReview, stores.CommitStatus),
		Reaction:         NewReactionService(stores.Reaction),
		TOTP:             NewTOTPService(stores.User),
		AuditLog:         NewAuditService(stores.AuditLog),
		SSO:              NewSSOService(stores.SSO, stores.User, cfg.Auth, siteSettingSvc),
	}
}
