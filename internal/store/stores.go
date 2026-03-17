package store

import (
	"database/sql"

	storedb "github.com/mkappworks/cloudzilla/internal/store/db"
)

type Stores struct {
	User         *UserStore
	Repo         *RepoStore
	Issue        *IssueStore
	Pull         *PullStore
	Comment      *CommentStore
	SSHKey       *SSHKeyStore
	Org          *OrgStore
	Webhook      *WebhookStore
	Notification *NotificationStore
}

func New(database *sql.DB) *Stores {
	q := storedb.New(database)
	return &Stores{
		User:         NewUserStore(q),
		Repo:         NewRepoStore(q, database),
		Issue:        NewIssueStore(q),
		Pull:         NewPullStore(q),
		Comment:      NewCommentStore(q),
		SSHKey:       NewSSHKeyStore(q),
		Org:          NewOrgStore(database),
		Webhook:      NewWebhookStore(database),
		Notification: NewNotificationStore(database),
	}
}
