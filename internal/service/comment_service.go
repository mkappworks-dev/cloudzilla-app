package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// CommentService manages issue and PR comment creation, editing, and deletion.
type CommentService struct {
	comments *store.CommentStore
	mentions *store.MentionStore
	users    *UserService
	notifs   *NotificationService
}

// NewCommentService creates a CommentService backed by the given stores.
func NewCommentService(
	comments *store.CommentStore,
	mentions *store.MentionStore,
	users *UserService,
	notifs *NotificationService,
) *CommentService {
	return &CommentService{
		comments: comments,
		mentions: mentions,
		users:    users,
		notifs:   notifs,
	}
}

func (s *CommentService) CreateForIssue(ctx context.Context, repo model.Repository, issueID int64, issueNumber int, authorID int64, authorName, body string) (*model.Comment, error) {
	c := &model.Comment{
		RepoID:     repo.ID,
		IssueID:    &issueID,
		AuthorID:   authorID,
		AuthorName: authorName,
		Body:       body,
	}
	if err := s.comments.Create(ctx, c); err != nil {
		return nil, err
	}
	s.processMentions(ctx, repo, c, issueNumber, authorID, authorName)
	return c, nil
}

func (s *CommentService) CreateForPull(ctx context.Context, repo model.Repository, pullID int64, pullNumber int, authorID int64, authorName, body string) (*model.Comment, error) {
	c := &model.Comment{
		RepoID:     repo.ID,
		PullID:     &pullID,
		AuthorID:   authorID,
		AuthorName: authorName,
		Body:       body,
	}
	if err := s.comments.Create(ctx, c); err != nil {
		return nil, err
	}
	s.processMentions(ctx, repo, c, pullNumber, authorID, authorName)
	return c, nil
}

func (s *CommentService) GetByID(ctx context.Context, id int64) (*model.Comment, error) {
	return s.comments.GetByID(ctx, id)
}

func (s *CommentService) Update(ctx context.Context, id, callerID int64, body string) (*model.Comment, error) {
	c, err := s.comments.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("comment not found: %w", err)
	}
	if c.AuthorID != callerID {
		return nil, fmt.Errorf("forbidden")
	}
	return s.comments.Update(ctx, id, body)
}

func (s *CommentService) ListByIssue(ctx context.Context, issueID int64) ([]model.Comment, error) {
	return s.comments.ListByIssue(ctx, issueID)
}

func (s *CommentService) ListByPull(ctx context.Context, pullID int64) ([]model.Comment, error) {
	return s.comments.ListByPull(ctx, pullID)
}

func (s *CommentService) Delete(ctx context.Context, id int64) error {
	return s.comments.Delete(ctx, id)
}

// processMentions extracts @mentions from the comment body, fires notifications,
// and persists mention rows. Errors are silently dropped (best-effort).
// subjectNumber is the repo-scoped display number (issue.Number or pull.Number),
// used to build the correct notification URL.
func (s *CommentService) processMentions(ctx context.Context, repo model.Repository, c *model.Comment, subjectNumber int, actorID int64, actorName string) {
	usernames := parseMentions(c.Body)
	if len(usernames) == 0 {
		return
	}
	var userIDs []int64
	for _, username := range usernames {
		u, err := s.users.GetByUsername(ctx, username)
		if err != nil {
			continue // user does not exist — skip
		}
		if u.ID == actorID {
			continue // no self-notifications
		}
		var subjectURL string
		if c.IssueID != nil {
			subjectURL = fmt.Sprintf("/%s/%s/issues/%d", repo.OwnerName, repo.Name, subjectNumber)
		} else if c.PullID != nil {
			subjectURL = fmt.Sprintf("/%s/%s/pulls/%d", repo.OwnerName, repo.Name, subjectNumber)
		}
		s.notifs.NotifyMention(ctx, repo, actorID, actorName, u.ID, subjectURL)
		userIDs = append(userIDs, u.ID)
	}
	_ = s.mentions.CreateBatch(ctx, c.ID, userIDs)
}
