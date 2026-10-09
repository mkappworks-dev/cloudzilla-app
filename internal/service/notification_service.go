package service

import (
	"context"
	"errors"
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
	threads  *ThreadSubscriptionService
}

// NewNotificationService creates a NotificationService backed by the given stores and services.
func NewNotificationService(notifs *store.NotificationStore, watches *store.WatchStore, repos *RepoService, emailSvc *EmailService, userSvc *UserService) *NotificationService {
	return &NotificationService{notifs: notifs, watches: watches, repos: repos, emailSvc: emailSvc, userSvc: userSvc}
}

// WithThreadSubscriptions makes fan-out honour per-thread subscriptions and mutes.
func (s *NotificationService) WithThreadSubscriptions(t *ThreadSubscriptionService) *NotificationService {
	s.threads = t
	return s
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

// notifyThread sends n to everyone following one thread: its author, its
// subscribed users and, if withWatchers, the repo's non-ignoring watchers. The
// actor and users who muted the thread are left out. Email goes only to the
// author and to users with a subscribed row, so a bare watcher gets the
// in-app copy and the digest.
func (s *NotificationService) notifyThread(ctx context.Context, repo *model.Repository, n *model.Notification, kind string, authorID int64, withWatchers bool) {
	n.SubjectKind = kind
	var muted, subscribed []int64
	if s.threads != nil {
		var err error
		if muted, err = s.threads.MutedUsers(ctx, n.RepoID, kind, n.SubjectID); err != nil {
			slog.Error("notifyThread: failed to list muted users", "repo_id", n.RepoID, "kind", kind, "number", n.SubjectID, "error", err)
			return
		}
		if subscribed, err = s.threads.SubscribedUsers(ctx, n.RepoID, kind, n.SubjectID); err != nil {
			slog.Error("notifyThread: failed to list subscribers", "repo_id", n.RepoID, "kind", kind, "number", n.SubjectID, "error", err)
			return
		}
	}
	var watchers []int64
	if withWatchers && s.watches != nil {
		var err error
		if watchers, err = s.watches.ListWatchersByRepo(ctx, n.RepoID, ""); err != nil {
			slog.Error("notifyThread: failed to list watchers", "repo_id", n.RepoID, "error", err)
			return
		}
	}

	emailed := map[int64]bool{authorID: true}
	for _, uid := range subscribed {
		emailed[uid] = true
	}
	skip := map[int64]bool{n.ActorID: true}
	for _, uid := range muted {
		skip[uid] = true
	}
	for _, uid := range slices.Concat([]int64{authorID}, subscribed, watchers) {
		if skip[uid] {
			continue
		}
		skip[uid] = true
		recipient := *n
		recipient.UserID = uid
		if s.create(ctx, repo, &recipient) && emailed[uid] {
			s.sendEmailAsync(recipient)
		}
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
	return p, nil
}

// MarkReadMany marks the given notifications read; ids that aren't userID's are ignored.
func (s *NotificationService) MarkReadMany(ctx context.Context, userID int64, ids []int64) error {
	return s.notifs.MarkReadMany(ctx, userID, ids)
}

// MuteThreads mutes each thread behind userID's notifications among ids and marks all of those
// threads' unread notifications read. Ids that aren't userID's, and rows with no thread, are ignored.
// The repo watch is untouched.
func (s *NotificationService) MuteThreads(ctx context.Context, userID int64, ids []int64) error {
	if s.threads == nil {
		return errors.New("mute threads: thread subscriptions not configured")
	}
	threads, err := s.notifs.ThreadsOf(ctx, userID, ids)
	if err != nil {
		return err
	}
	for _, t := range threads {
		if err := s.threads.Set(ctx, userID, t.RepoID, t.Kind, t.Number, model.ThreadStateMuted); err != nil {
			return err
		}
	}
	return s.notifs.MarkThreadsRead(ctx, userID, ids)
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
	s.notifyThread(ctx, &repo, n, model.ThreadKindIssue, issue.AuthorID, true)
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
	s.notifyThread(ctx, &repo, n, model.ThreadKindPull, pr.AuthorID, true)
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
	s.notifyThread(ctx, &repo, n, model.ThreadKindIssue, issue.AuthorID, true)
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
	s.notifyThread(ctx, &repo, n, model.ThreadKindPull, pr.AuthorID, true)
}

// NotifyMention fires a mention notification for mentionedUserID and
// subscribes them to the thread. It ignores a mute: a mention is addressed to
// the user. Silent if actorID == mentionedUserID.
func (s *NotificationService) NotifyMention(ctx context.Context, repo model.Repository, actorID int64, actorName string, mentionedUserID int64, kind string, subjectNumber int, subjectURL string) {
	if actorID == mentionedUserID {
		return
	}
	n := &model.Notification{
		UserID:      mentionedUserID,
		ActorID:     actorID,
		ActorName:   actorName,
		Type:        model.NotifMention,
		RepoID:      repo.ID,
		RepoName:    repo.Name,
		OwnerName:   repo.OwnerName,
		SubjectID:   int64(subjectNumber),
		SubjectURL:  subjectURL,
		SubjectKind: kind,
	}
	if !s.create(ctx, &repo, n) {
		return
	}
	if s.threads != nil {
		if err := s.threads.SubscribeOnMention(ctx, mentionedUserID, repo.ID, kind, int64(subjectNumber)); err != nil {
			slog.Error("NotifyMention: failed to subscribe", "user_id", mentionedUserID, "repo_id", repo.ID, "error", err)
		}
	}
	s.sendEmailAsync(*n)
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
	s.notifyThread(ctx, &repo, n, model.ThreadKindPull, pr.AuthorID, true)
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

// NotifyDiscussionReply tells the discussion's author and subscribers. Watchers
// are not told: discussion replies never reached them.
func (s *NotificationService) NotifyDiscussionReply(ctx context.Context, repo model.Repository, discussion model.Discussion, actorID int64, actorName string) {
	n := &model.Notification{
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
	s.notifyThread(ctx, &repo, n, model.ThreadKindDiscussion, discussion.AuthorID, false)
}
