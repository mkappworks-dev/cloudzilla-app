package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type NotificationService struct {
	notifs   *store.NotificationStore
	watches  *store.WatchStore
	emailSvc *EmailService
	userSvc  *UserService
}

func NewNotificationService(notifs *store.NotificationStore, watches *store.WatchStore, emailSvc *EmailService, userSvc *UserService) *NotificationService {
	return &NotificationService{notifs: notifs, watches: watches, emailSvc: emailSvc, userSvc: userSvc}
}

// fanOutToWatchers sends n to all non-ignoring watchers of n.RepoID,
// excluding the actor and skipUserID (pass 0 to skip no-one extra).
func (s *NotificationService) fanOutToWatchers(ctx context.Context, n *model.Notification, skipUserID int64) {
	if s.watches == nil {
		return
	}
	watchers, err := s.watches.ListWatchersByRepo(ctx, n.RepoID, "")
	if err != nil {
		slog.Error("fanOutToWatchers: failed to list watchers", "repo_id", n.RepoID, "error", err)
		return
	}
	for _, uid := range watchers {
		if uid == n.ActorID || uid == skipUserID {
			continue
		}
		copy := *n
		copy.UserID = uid
		if err := s.notifs.Create(ctx, &copy); err != nil {
			slog.Error("fanOutToWatchers: failed to create notification", "user_id", uid, "repo_id", n.RepoID, "error", err)
		}
	}
}

func (s *NotificationService) sendEmailAsync(notif model.Notification) {
	go func() {
		u, err := s.userSvc.GetByID(context.Background(), notif.UserID)
		if err != nil || u.EmailDigest != "immediate" {
			return
		}
		if err := s.emailSvc.SendNotification(context.Background(), u, &notif); err != nil {
			slog.Error("sendEmailAsync: failed to send notification email", "user_id", notif.UserID, "notif_type", notif.Type, "error", err)
		}
	}()
}

func (s *NotificationService) List(ctx context.Context, userID int64) ([]model.Notification, error) {
	return s.notifs.ListByUser(ctx, userID)
}

// ListUnreadByUser returns all unread notifications for a user.
func (s *NotificationService) ListUnreadByUser(ctx context.Context, userID int64) ([]model.Notification, error) {
	return s.notifs.ListUnreadByUser(ctx, userID)
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
	n := &model.Notification{
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifIssueComment,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(issue.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
	}
	if actorID != issue.AuthorID {
		n.UserID = issue.AuthorID
		if err := s.notifs.Create(ctx, n); err != nil {
			slog.Error("NotifyIssueComment: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
		} else {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, n, issue.AuthorID)
}

func (s *NotificationService) NotifyPRComment(ctx context.Context, repo model.Repository, pr model.PullRequest, actorID int64, actorName string) {
	n := &model.Notification{
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifPRComment,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(pr.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
	}
	if actorID != pr.AuthorID {
		n.UserID = pr.AuthorID
		if err := s.notifs.Create(ctx, n); err != nil {
			slog.Error("NotifyPRComment: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
		} else {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, n, pr.AuthorID)
}

func (s *NotificationService) NotifyIssueStateChange(ctx context.Context, repo model.Repository, issue model.Issue, actorID int64, actorName string) {
	notifType := model.NotifIssueClosed
	if issue.State == model.IssueStateOpen {
		notifType = model.NotifIssueReopened
	}
	n := &model.Notification{
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       notifType,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(issue.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
	}
	if actorID != issue.AuthorID {
		n.UserID = issue.AuthorID
		if err := s.notifs.Create(ctx, n); err != nil {
			slog.Error("NotifyIssueStateChange: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
		} else {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, n, issue.AuthorID)
}

func (s *NotificationService) NotifyPRReview(ctx context.Context, repo model.Repository, pr model.PullRequest, actorID int64, actorName string) {
	n := &model.Notification{
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifPRReview,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(pr.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
	}
	if actorID != pr.AuthorID {
		n.UserID = pr.AuthorID
		if err := s.notifs.Create(ctx, n); err != nil {
			slog.Error("NotifyPRReview: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
		} else {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, n, pr.AuthorID)
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
	if err := s.notifs.Create(ctx, n); err != nil {
		slog.Error("NotifyMention: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
	} else {
		s.sendEmailAsync(*n)
	}
}

func (s *NotificationService) NotifyPRStateChange(ctx context.Context, repo model.Repository, pr model.PullRequest, actorID int64, actorName string) {
	notifType := model.NotifPRClosed
	if pr.State == model.PRStateMerged {
		notifType = model.NotifPRMerged
	}
	n := &model.Notification{
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       notifType,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(pr.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
	}
	if actorID != pr.AuthorID {
		n.UserID = pr.AuthorID
		if err := s.notifs.Create(ctx, n); err != nil {
			slog.Error("NotifyPRStateChange: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
		} else {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, n, pr.AuthorID)
}
