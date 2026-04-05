package store

import (
	"database/sql"

	storedb "github.com/mkappworks/cloudzilla/internal/store/db"
)

type Stores struct {
	User             *UserStore
	Repo             *RepoStore
	Issue            *IssueStore
	Pull             *PullStore
	Comment          *CommentStore
	SSHKey           *SSHKeyStore
	Org              *OrgStore
	Webhook          *WebhookStore
	Notification     *NotificationStore
	SiteSetting      *SiteSettingStore
	Invitation       *InvitationStore
	Label            *LabelStore
	Assignee         *AssigneeStore
	Star             *StarStore
	Release          *ReleaseStore
	CommitStatus     *CommitStatusStore
	Milestone        *MilestoneStore
	PullReview       *PullReviewStore
	PullLineComment  *PullLineCommentStore
	Search           *SearchStore
	AccessToken      *AccessTokenStore
	DeployKey        *DeployKeyStore
	BranchProtection *BranchProtectionStore
	Reaction         *ReactionStore
	AuditLog         *AuditLogStore
	Project          *ProjectStore
	SSO              *SSOStore
	Mention           *MentionStore
	SavedReply        *SavedReplyStore
	OAuthApp          *OAuthAppStore
	OAuthAuthorization *OAuthAuthorizationStore
	Watch              *WatchStore
	Event              *EventStore
	Discussion         *DiscussionStore
	Gist               *GistStore
	Topic              *TopicStore
	CodeSearch         *CodeSearchStore
}

func New(database *sql.DB) *Stores {
	q := storedb.New(database)
	return &Stores{
		User:             NewUserStore(q, database),
		Repo:             NewRepoStore(q, database),
		Issue:            NewIssueStore(q, database),
		Pull:             NewPullStore(q, database),
		Comment:          NewCommentStore(q),
		SSHKey:           NewSSHKeyStore(q),
		Org:              NewOrgStore(database),
		Webhook:          NewWebhookStore(database),
		Notification:     NewNotificationStore(database),
		SiteSetting:      NewSiteSettingStore(database),
		Invitation:       NewInvitationStore(database),
		Label:            NewLabelStore(database),
		Assignee:         NewAssigneeStore(database),
		Star:             NewStarStore(database),
		Release:          NewReleaseStore(database),
		CommitStatus:     NewCommitStatusStore(database),
		Milestone:        NewMilestoneStore(database),
		PullReview:       NewPullReviewStore(database),
		PullLineComment:  NewPullLineCommentStore(database),
		Search:           NewSearchStore(database),
		AccessToken:      NewAccessTokenStore(database),
		DeployKey:        NewDeployKeyStore(database),
		BranchProtection: NewBranchProtectionStore(database),
		Reaction:         NewReactionStore(database),
		AuditLog:         NewAuditLogStore(database),
		Project:          NewProjectStore(database),
		SSO:              NewSSOStore(database),
		Mention:            NewMentionStore(database),
		SavedReply:         NewSavedReplyStore(database),
		OAuthApp:           NewOAuthAppStore(database),
		OAuthAuthorization: NewOAuthAuthorizationStore(database),
		Watch:              NewWatchStore(database),
		Event:              NewEventStore(database),
		Discussion:         NewDiscussionStore(database),
		Gist:               NewGistStore(database),
		Topic:              NewTopicStore(database),
		CodeSearch:         NewCodeSearchStore(database),
	}
}
