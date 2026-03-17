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
}

func New(stores *store.Stores, cfg *config.Config) *Services {
	return &Services{
		User:         NewUserService(stores.User, cfg.Auth),
		Repo:         NewRepoService(stores.Repo, stores.User, stores.Org, cfg.Git),
		Issue:        NewIssueService(stores.Issue, stores.Repo),
		Pull:         NewPullService(stores.Pull, stores.Repo),
		Comment:      NewCommentService(stores.Comment),
		SSHKey:       NewSSHKeyService(stores.SSHKey, stores.User),
		Code:         NewCodeService(cfg.Git),
		Org:          NewOrgService(stores.Org, stores.Repo, stores.User, cfg.Git),
		Webhook:      NewWebhookService(stores.Webhook),
		Notification: NewNotificationService(stores.Notification),
		SiteSetting:  NewSiteSettingService(stores.SiteSetting, stores.User),
		Invitation:   NewInvitationService(stores.Invitation),
	}
}
