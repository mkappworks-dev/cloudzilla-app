package service

import (
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type Services struct {
	User    *UserService
	Repo    *RepoService
	Issue   *IssueService
	Pull    *PullService
	Comment *CommentService
	SSHKey  *SSHKeyService
}

func New(stores *store.Stores, cfg *config.Config) *Services {
	return &Services{
		User:    NewUserService(stores.User, cfg.Auth),
		Repo:    NewRepoService(stores.Repo, stores.User, cfg.Git),
		Issue:   NewIssueService(stores.Issue, stores.Repo),
		Pull:    NewPullService(stores.Pull, stores.Repo),
		Comment: NewCommentService(stores.Comment),
		SSHKey:  NewSSHKeyService(stores.SSHKey, stores.User),
	}
}
