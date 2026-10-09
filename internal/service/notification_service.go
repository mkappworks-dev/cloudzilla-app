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

const NotificationsPerPage = 25

// NotificationPage is one page of the inbox plus the counts the filter sidebar shows.
type NotificationPage struct {
	Items       []model.Notification
	Filter      string
	Page        int
	TotalPages  int
	Total       int // matching the filter
	InboxCount  int
	UnreadCount int
	ReadCount   int

	// WatchedRepos holds the repos on this page that the user can still unsubscribe from.
	WatchedRepos map[int64]bool
}

// ListPage returns the requested page of userID's notifications. An unknown
// filter means the inbox, and a page past the end clamps to the last page.
func (s *NotificationService) ListPage(ctx context.Context, userID int64, filter string, page int) (NotificationPage, error) {
	if filter != store.NotifFilterUnread && filter != store.NotifFilterRead {
		filter = store.NotifFilterInbox
	}
	inbox, err := s.notifs.CountByFilter(ctx, userID, store.NotifFilterInbox)
	if err != nil {
		return NotificationPage{}, err
	}
	unread, err := s.notifs.CountUnread(ctx, userID)
	if err != nil {
		return NotificationPage{}, err
	}
	p := NotificationPage{Filter: filter, InboxCount: inbox, UnreadCount: unread, ReadCount: inbox - unread}
	switch filter {
	case store.NotifFilterUnread:
		p.Total = unread
	case store.NotifFilterRead:
		p.Total = p.ReadCount
	default:
		p.Total = inbox
	}
	p.TotalPages = max(1, (p.Total+NotificationsPerPage-1)/NotificationsPerPage)
	p.Page = min(max(page, 1), p.TotalPages)
	p.Items, err = s.notifs.ListPage(ctx, userID, filter, NotificationsPerPage, (p.Page-1)*NotificationsPerPage)
	if err != nil {
		return NotificationPage{}, err
	}
	repoIDs := make([]int64, 0, len(p.Items))
	for _, n := range p.Items {
		if n.Type != model.NotifRepoTransfer {
			repoIDs = append(repoIDs, n.RepoID)
		}
	}
	watched, err := s.watches.ListWatchedAmong(ctx, userID, repoIDs)
	if err != nil {
		return NotificationPage{}, err
	}
	p.WatchedRepos = make(map[int64]bool, len(watched))
	for _, id := range watched {
		p.WatchedRepos[id] = true
	}
	return p, nil
}

// MarkReadMany marks the given notifications read; ids that aren't userID's are ignored.
func (s *NotificationService) MarkReadMany(ctx context.Context, userID int64, ids []int64) error {
	return s.notifs.MarkReadMany(ctx, userID, ids)
}

// UnsubscribeFromRepos stops userID watching each repo behind the given notifications.
// Unsubscribing is per repo, not per thread: no thread-level subscriptions exist.
func (s *NotificationService) UnsubscribeFromRepos(ctx context.Context, userID int64, ids []int64) error {
	repoIDs, err := s.notifs.RepoIDsOf(ctx, userID, ids)
	if err != nil {
		return err
	}
	return s.watches.DeleteMany(ctx, userID, repoIDs)
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
		ActorID:      actorID,
		ActorName:    actorName,
		Type:         model.NotifIssueComment,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(issue.Number),
		SubjectURL:   fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
		SubjectTitle: issue.Title,
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
		ActorID:      actorID,
		ActorName:    actorName,
		Type:         model.NotifPRComment,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(pr.Number),
		SubjectURL:   fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
		SubjectTitle: pr.Title,
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
		ActorID:      actorID,
		ActorName:    actorName,
		Type:         notifType,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(issue.Number),
		SubjectURL:   fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, issue.Number),
		SubjectTitle: issue.Title,
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
		ActorID:      actorID,
		ActorName:    actorName,
		Type:         model.NotifPRReview,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(pr.Number),
		SubjectURL:   fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
		SubjectTitle: pr.Title,
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
		ActorID:      actorID,
		ActorName:    actorName,
		Type:         notifType,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(pr.Number),
		SubjectURL:   fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pr.Number),
		SubjectTitle: pr.Title,
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
		UserID:       discussion.AuthorID,
		ActorID:      actorID,
		ActorName:    actorName,
		Type:         model.NotifDiscussionReply,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(discussion.Number),
		SubjectURL:   fmt.Sprintf("/%s/%s/discussions/%d", repo.OwnerName, repo.Name, discussion.Number),
		SubjectTitle: discussion.Title,
	}
	if s.create(ctx, &repo, n) {
		s.sendEmailAsync(*n)
	}
}
