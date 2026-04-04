package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type NotificationService struct {
	notifs *store.NotificationStore
}

func NewNotificationService(notifs *store.NotificationStore) *NotificationService {
	return &NotificationService{notifs: notifs}
}

func (s *NotificationService) List(ctx context.Context, userID int64) ([]model.Notification, error) {
	return s.notifs.ListByUser(ctx, userID)
}

func (s *NotificationService) CountUnread(ctx context.Context, userID int64) (int, error) {
	return s.notifs.CountUnread(ctx, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, id, userID int64) error {
	return s.notifs.MarkRead(ctx, id, userID)
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID int64) error {
	return s.notifs.MarkAllRead(ctx, userID)
}

func (s *NotificationService) NotifyIssueComment(ctx context.Context, repo model.Repository, issue model.Issue, actorID int64, actorName string) {
	if actorID == issue.AuthorID {
		return
	}
	n := &model.Notification{
		UserID:     issue.AuthorID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifIssueComment,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(issue.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
	}
	_ = s.notifs.Create(ctx, n)
}

func (s *NotificationService) NotifyPRComment(ctx context.Context, repo model.Repository, pr model.PullRequest, actorID int64, actorName string) {
	if actorID == pr.AuthorID {
		return
	}
	n := &model.Notification{
		UserID:     pr.AuthorID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifPRComment,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(pr.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
	}
	_ = s.notifs.Create(ctx, n)
}

func (s *NotificationService) NotifyIssueStateChange(ctx context.Context, repo model.Repository, issue model.Issue, actorID int64, actorName string) {
	if actorID == issue.AuthorID {
		return
	}
	notifType := model.NotifIssueClosed
	if issue.State == model.IssueStateOpen {
		notifType = model.NotifIssueReopened
	}
	n := &model.Notification{
		UserID:     issue.AuthorID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       notifType,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(issue.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
	}
	_ = s.notifs.Create(ctx, n)
}

func (s *NotificationService) NotifyPRReview(ctx context.Context, repo model.Repository, pr model.PullRequest, actorID int64, actorName string) {
	if actorID == pr.AuthorID {
		return
	}
	n := &model.Notification{
		UserID:     pr.AuthorID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifPRReview,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(pr.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
	}
	_ = s.notifs.Create(ctx, n)
}

// NotifyMention fires a mention notification for mentionedUserID.
// Silent if actorID == mentionedUserID.
func (s *NotificationService) NotifyMention(ctx context.Context, repo model.Repository, actorID int64, actorName string, mentionedUserID int64, subjectURL string) {
	if actorID == mentionedUserID {
		return
	}
	n := &model.Notification{
		UserID:     mentionedUserID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifMention,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectURL: subjectURL,
	}
	_ = s.notifs.Create(ctx, n)
}

func (s *NotificationService) NotifyPRStateChange(ctx context.Context, repo model.Repository, pr model.PullRequest, actorID int64, actorName string) {
	if actorID == pr.AuthorID {
		return
	}
	notifType := model.NotifPRClosed
	if pr.State == model.PRStateMerged {
		notifType = model.NotifPRMerged
	}
	n := &model.Notification{
		UserID:     pr.AuthorID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       notifType,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(pr.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
	}
	_ = s.notifs.Create(ctx, n)
}
