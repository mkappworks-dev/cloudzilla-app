package service

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// Services bundles all application service instances used by HTTP handlers.
type Services struct {
	User             *UserService
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
	Label            *LabelService
	Assignee         *AssigneeService
	Star             *StarService
	Release          *ReleaseService
	CommitStatus     *CommitStatusService
	Milestone        *MilestoneService
	PullReview       *PullReviewService
	PullLineComment  *PullLineCommentService
	Search           *SearchService
	AccessToken      *AccessTokenService
	DeployKey        *DeployKeyService
	BranchProtection *BranchProtectionService
	Reaction         *ReactionService
	TOTP             *TOTPService
	AuditLog         *AuditService
	Project          *ProjectService
	SSO              *SSOService
	SavedReply       *SavedReplyService
	Email            *EmailService
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
	Attention        *AttentionService
	Language         *LanguageService
}

// New constructs and wires all services from the given stores and configuration.
func New(stores *store.Stores, cfg *config.Config) *Services {
	code := NewCodeService(cfg.Git)
	languageSvc := NewLanguageService(code)
	index := NewIndexService(stores.CodeSearch, code)
	commitStatsSvc := NewCommitStatsService(stores.CommitStats, stores.User)
	attentionSvc := NewAttentionService(stores.Issue)
	repoSvc := NewRepoService(stores.Repo, stores.User, stores.Org, commitStatsSvc, cfg.Git)
	siteSettingSvc := NewSiteSettingService(stores.SiteSetting, stores.User)
	userSvc := NewUserService(stores.User, cfg.Auth)
	emailSvc := NewEmailService(cfg.SMTP)
	notifSvc := NewNotificationService(stores.Notification, stores.Watch, emailSvc, userSvc)
	return &Services{
		User:             userSvc,
		Repo:             repoSvc,
		Issue:            NewIssueService(stores.Issue, stores.Repo, repoSvc),
		Pull:             NewPullService(stores.Pull, stores.Repo, repoSvc),
		Comment:          NewCommentService(stores.Comment, stores.Mention, userSvc, notifSvc),
		SSHKey:           NewSSHKeyService(stores.SSHKey, stores.User),
		Code:             code,
		Org:              NewOrgService(stores.Org, stores.Repo, stores.User, cfg.Git),
		Webhook:          NewWebhookService(stores.Webhook),
		Notification:     notifSvc,
		SiteSetting:      siteSettingSvc,
		Invitation:       NewInvitationService(stores.Invitation),
		Label:            NewLabelService(stores.Label, stores.Repo, stores.Issue, stores.Pull),
		Assignee:         NewAssigneeService(stores.Assignee, stores.Repo, stores.Issue, stores.Pull, stores.User),
		Star:             NewStarService(stores.Star, stores.Repo, stores.User),
		Release:          NewReleaseService(stores.Release, stores.Repo, code),
		CommitStatus:     NewCommitStatusService(stores.CommitStatus, stores.Repo),
		Milestone:        NewMilestoneService(stores.Milestone, stores.Repo),
		PullReview:       NewPullReviewService(stores.PullReview, stores.Pull, stores.Repo),
		PullLineComment:  NewPullLineCommentService(stores.PullLineComment, stores.Pull, stores.Repo),
		Search:           NewSearchService(stores.Search),
		AccessToken:      NewAccessTokenService(stores.AccessToken, stores.User),
		DeployKey:        NewDeployKeyService(stores.DeployKey, stores.SSHKey),
		BranchProtection: NewBranchProtectionService(stores.BranchProtection, stores.PullReview, stores.CommitStatus),
		Reaction:         NewReactionService(stores.Reaction),
		TOTP:             NewTOTPService(stores.User),
		AuditLog:         NewAuditService(stores.AuditLog),
		Project:          NewProjectService(stores.Project, repoSvc),
		SSO:              NewSSOService(stores.SSO, stores.User, cfg.Auth, siteSettingSvc),
		SavedReply:       NewSavedReplyService(stores.SavedReply),
		Email:            emailSvc,
		OAuthApp:         NewOAuthAppService(stores.OAuthApp, stores.OAuthAuthorization),
		Watch:            NewWatchService(stores.Watch, stores.Repo),
		Event:            NewEventService(stores.Event, stores.User, stores.Repo),
		Discussion:       NewDiscussionService(stores.Discussion, stores.Repo),
		Gist:             NewGistService(stores.Gist),
		Topic:            NewTopicService(stores.Topic),
		Index:            index,
		Explore:          NewExploreService(stores.Explore),
		Dependency:       NewDependencyService(stores.Dependency, code),
		CommitStats:      commitStatsSvc,
		Attention:        attentionSvc,
		Language:         languageSvc,
	}
}
