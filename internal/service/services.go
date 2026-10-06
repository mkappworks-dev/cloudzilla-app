package service

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// Services bundles all application service instances used by HTTP handlers.
type Services struct {
	User             *UserService
	AdminUser        *AdminUserService
	Repo             *RepoService
	Issue            *IssueService
	Pull             *PullService
	Comment          *CommentService
	SSHKey           *SSHKeyService
	Code             *CodeService
	Org              *OrgService
	Webhook          *WebhookService
	Notification     *NotificationService
	SiteSetting      *SiteSettingService
	Invitation       *InvitationService
	Signup           *SignupService
	Label            *LabelService
	Assignee         *AssigneeService
	Star             *StarService
	Release          *ReleaseService
	CommitStatus     *CommitStatusService
	Milestone        *MilestoneService
	PullReview       *PullReviewService
	PullLineComment  *PullLineCommentService
	PullEvent        *PullEventService
	Search           *SearchService
	AccessToken      *AccessTokenService
	DeployKey        *DeployKeyService
	BranchProtection *BranchProtectionService
	Reaction         *ReactionService
	TOTP             *TOTPService
	OAuthLink        *OAuthLinkService
	Reauth           *ReauthService
	AuditLog         *AuditService
	Project          *ProjectService
	SSO              *SSOService
	SavedReply       *SavedReplyService
	Email            *EmailService
	EmailVerifier    *EmailVerificationService
	PasswordReset    *PasswordResetService
	OAuthApp         *OAuthAppService
	Watch            *WatchService
	Event            *EventService
	Discussion       *DiscussionService
	Gist             *GistService
	Topic            *TopicService
	Index            *IndexService
	Explore          *ExploreService
	Dependency       *DependencyService
	CommitStats      *CommitStatsService
	ContributorStats *ContributorStatsService
	Attention        *AttentionService
	Language         *LanguageService
	Import           *ImportService
	Health           *HealthService
}

// New constructs and wires all services from the given stores and configuration.
func New(stores *store.Stores, cfg *config.Config) *Services {
	code := NewCodeService(cfg.Git)
	index := NewIndexService(stores.CodeSearch, code)
	commitStatsSvc := NewCommitStatsService(stores.CommitStats, stores.User)
	contributorStatsSvc := NewContributorStatsService(stores.ContributorStats, stores.User)
	attentionSvc := NewAttentionService(stores.Issue).WithPullDeps(stores.Pull, stores.PullReview, stores.Mention).WithUserStore(stores.User)
	repoSvc := NewRepoService(stores.Repo, stores.User, stores.Org, contributorStatsSvc, code, cfg.Git).WithPullStore(stores.Pull).
		WithTransferStore(stores.RepoTransfer).WithNoreplyHostFrom(cfg.Server.BaseURL)
	orgSvc := NewOrgService(stores.Org, stores.Repo, stores.User, cfg.Git).WithStarStore(stores.Star).WithRepoService(repoSvc)
	languageSvc := NewLanguageService(code, repoSvc)
	repoSvc.WithLanguageService(languageSvc)
	siteSettingSvc := NewSiteSettingService(stores.SiteSetting, stores.User)
	emailSvc := NewEmailService(cfg.SMTP)
	emailVerificationSvc := NewEmailVerificationService(stores.EmailVerification, stores.User, emailSvc, cfg.Server.BaseURL)
	userSvc := NewUserService(stores.User, cfg.Auth).WithRepoService(repoSvc).WithNoreplyHostFrom(cfg.Server.BaseURL).
		WithEmailVerification(emailVerificationSvc).WithSecurityNotices(emailSvc)
	totpSvc := NewTOTPService(stores.User).WithSecurityNotices(emailSvc)
	ssoSvc := NewSSOService(stores.SSO, stores.User, cfg.Auth, siteSettingSvc)
	reauthSvc := NewReauthService(stores.User, totpSvc).WithEmailCodes(emailSvc).
		WithProviderSignIn(stores.OAuthState, ssoSvc, cfg.OAuth.GoogleClientID != "")
	userSvc.WithReauth(reauthSvc)
	notifSvc := NewNotificationService(stores.Notification, stores.Watch, repoSvc, emailSvc, userSvc)
	commitStatusSvc := NewCommitStatusService(stores.CommitStatus, stores.Repo, stores.Pull, stores.BranchProtection, code)
	pullSvc := NewPullService(stores.Pull, stores.Repo, repoSvc).WithCIDeps(
		code, commitStatusSvc, stores.PullReview, stores.Label, stores.Assignee, stores.Comment,
	).WithReviewerDeps(stores.ContributorStats, stores.User).WithMentionStore(stores.Mention)
	auditSvc := NewAuditService(stores.AuditLog)
	return &Services{
		User:             userSvc,
		AdminUser:        NewAdminUserService(stores.User, userSvc, auditSvc).WithSecurityNotices(emailSvc),
		Repo:             repoSvc,
		Issue:            NewIssueService(stores.Issue, stores.Repo, stores.Pull, repoSvc).WithMentionStore(stores.Mention),
		Pull:             pullSvc,
		Comment:          NewCommentService(stores.Comment, stores.Mention, userSvc, notifSvc, repoSvc),
		SSHKey:           NewSSHKeyService(stores.SSHKey, stores.User),
		Code:             code,
		Org:              orgSvc,
		Webhook:          NewWebhookService(stores.Webhook, cfg.Webhook),
		Notification:     notifSvc,
		SiteSetting:      siteSettingSvc,
		Invitation:       NewInvitationService(stores.Invitation),
		Signup:           NewSignupService(stores.SignupToken, stores.User, emailSvc, cfg.Server.BaseURL),
		Label:            NewLabelService(stores.Label, stores.Repo, stores.Issue, stores.Pull, stores.Discussion),
		Assignee:         NewAssigneeService(stores.Assignee, stores.Repo, stores.Issue, stores.Pull, stores.User),
		Star:             NewStarService(stores.Star, stores.Repo, stores.User),
		Release:          NewReleaseService(stores.Release, stores.Repo, code),
		CommitStatus:     commitStatusSvc,
		Milestone:        NewMilestoneService(stores.Milestone, stores.Repo),
		PullReview:       NewPullReviewService(stores.PullReview, stores.Pull, stores.Repo, stores.BranchProtection),
		PullLineComment:  NewPullLineCommentService(stores.PullLineComment, stores.Pull, stores.Repo),
		PullEvent:        NewPullEventService(stores.PullEvent),
		Search:           NewSearchService(stores.Search),
		AccessToken:      NewAccessTokenService(stores.AccessToken, stores.User).WithAdminTargets(repoSvc, orgSvc),
		DeployKey:        NewDeployKeyService(stores.DeployKey, stores.SSHKey),
		BranchProtection: NewBranchProtectionService(stores.BranchProtection, stores.PullReview, stores.CommitStatus),
		Reaction:         NewReactionService(stores.Reaction),
		TOTP:             totpSvc,
		OAuthLink:        NewOAuthLinkService(stores.User, stores.OAuthState, totpSvc, emailSvc),
		Reauth:           reauthSvc,
		AuditLog:         auditSvc,
		Project:          NewProjectService(stores.Project, repoSvc),
		SSO:              ssoSvc,
		SavedReply:       NewSavedReplyService(stores.SavedReply),
		Email:            emailSvc,
		EmailVerifier:    emailVerificationSvc,
		PasswordReset:    NewPasswordResetService(stores.PasswordReset, stores.User, reauthSvc, emailSvc, cfg.Server.BaseURL),
		OAuthApp:         NewOAuthAppService(stores.OAuthApp, stores.OAuthAuthorization, stores.User),
		Watch:            NewWatchService(stores.Watch, stores.Repo),
		Event:            NewEventService(stores.Event, stores.User, stores.Repo),
		Discussion:       NewDiscussionService(stores.Discussion, stores.Repo),
		Gist:             NewGistService(stores.Gist),
		Topic:            NewTopicService(stores.Topic),
		Index:            index,
		Explore:          NewExploreService(stores.Explore),
		Dependency:       NewDependencyService(stores.Dependency, code),
		CommitStats:      commitStatsSvc,
		ContributorStats: contributorStatsSvc,
		Attention:        attentionSvc,
		Language:         languageSvc,
		Import:           NewImportService(repoSvc, cfg.Git, cfg.Import),
		Health:           NewHealthService(stores.Health, cfg.Git.ReposRoot),
	}
}
