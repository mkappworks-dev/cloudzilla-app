package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// NotificationService creates and delivers in-app and email notifications.
type NotificationService struct {
	notifs   *store.NotificationStore
	watches  *store.WatchStore
	repos    *RepoService
	emailSvc *EmailService
	userSvc  *UserService
}

// NewNotificationService creates a NotificationService backed by the given stores and services.
func NewNotificationService(notifs *store.NotificationStore, watches *store.WatchStore, repos *RepoService, emailSvc *EmailService, userSvc *UserService) *NotificationService {
	return &NotificationService{notifs: notifs, watches: watches, repos: repos, emailSvc: emailSvc, userSvc: userSvc}
}

// create stores n for n.UserID and reports whether it did. Read access is
// checked here, not when the user watched or authored the subject: a watch or
// an authorship outlives a revoked collaborator role or org ownership.
func (s *NotificationService) create(ctx context.Context, repo *model.Repository, n *model.Notification) bool {
	if !s.repos.CanRead(ctx, repo, &n.UserID) {
		return false
	}
	if err := s.notifs.Create(ctx, n); err != nil {
		slog.Error("failed to create notification", "type", n.Type, "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
		return false
	}
	return true
}

// fanOutToWatchers sends n to all non-ignoring watchers of repo,
// excluding the actor and skipUserID (pass 0 to skip no-one extra).
func (s *NotificationService) fanOutToWatchers(ctx context.Context, repo *model.Repository, n *model.Notification, skipUserID int64) {
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
		s.create(ctx, repo, &copy)
	}
}

func (s *NotificationService) sendEmailAsync(notif model.Notification) {
	go func() {
		u, err := s.userSvc.GetByID(context.Background(), notif.UserID)
		if err != nil {
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

func (s *NotificationService) ListUnreadForDigest(ctx context.Context, u *model.User, mode string) ([]model.Notification, error) {
	notifs, err := s.notifs.ListUnreadReadable(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(notifs, func(n model.Notification) bool { return !wantsEmail(u, n.Type, mode) }), nil
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
		if s.create(ctx, &repo, n) {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, &repo, n, issue.AuthorID)
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
		if s.create(ctx, &repo, n) {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, &repo, n, pr.AuthorID)
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
		if s.create(ctx, &repo, n) {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, &repo, n, issue.AuthorID)
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
		if s.create(ctx, &repo, n) {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, &repo, n, pr.AuthorID)
}

// NotifyMention fires a mention notification for mentionedUserID.
// Silent if actorID == mentionedUserID.
func (s *NotificationService) NotifyMention(ctx context.Context, repo model.Repository, actorID int64, actorName string, mentionedUserID int64, subjectNumber int, subjectURL string) {
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
		SubjectID:  int64(subjectNumber),
		SubjectURL: subjectURL,
	}
	if s.create(ctx, &repo, n) {
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
		if s.create(ctx, &repo, n) {
			s.sendEmailAsync(*n)
		}
	}
	s.fanOutToWatchers(ctx, &repo, n, pr.AuthorID)
}

// NotifyRepoTransfer tells t's recipient that a repository awaits their answer.
// It skips create's read check: the recipient can't read a private repo until
// they accept it.
func (s *NotificationService) NotifyRepoTransfer(ctx context.Context, t model.RepoTransfer) {
	n := &model.Notification{
		UserID:     t.RecipientID,
		ActorID:    t.RequesterID,
		ActorName:  t.RequesterName,
		Type:       model.NotifRepoTransfer,
		RepoID:     t.RepoID,
		RepoName:   t.RepoName,
		OwnerName:  t.OwnerName,
		SubjectID:  t.ID,
		SubjectURL: "/repos/transfers",
	}
	if err := s.notifs.Create(ctx, n); err != nil {
		slog.Error("NotifyRepoTransfer: failed to create notification", "user_id", n.UserID, "repo_id", n.RepoID, "error", err)
	} else {
		s.sendEmailAsync(*n)
	}
}

func (s *NotificationService) NotifyDiscussionReply(ctx context.Context, repo model.Repository, discussion model.Discussion, actorID int64, actorName string) {
	if actorID == discussion.AuthorID {
		return
	}
	n := &model.Notification{
		UserID:     discussion.AuthorID,
		ActorID:    actorID,
		ActorName:  actorName,
		Type:       model.NotifDiscussionReply,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectID:  int64(discussion.Number),
		SubjectURL: fmt.Sprintf("/%s/%s/discussions/%d", repo.OwnerName, repo.Name, discussion.Number),
	}
	if s.create(ctx, &repo, n) {
		s.sendEmailAsync(*n)
	}
}
