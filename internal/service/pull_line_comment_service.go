package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type PullLineCommentService struct {
	comments *store.PullLineCommentStore
	pulls    *store.PullStore
	repos    *store.RepoStore
}

func NewPullLineCommentService(comments *store.PullLineCommentStore, pulls *store.PullStore, repos *store.RepoStore) *PullLineCommentService {
	return &PullLineCommentService{comments: comments, pulls: pulls, repos: repos}
}

func (s *PullLineCommentService) Create(ctx context.Context, owner, repoName string, pullNumber int, authorID int64, authorName, path, diffSide string, line int, body string) (*model.PullLineComment, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return nil, fmt.Errorf("pull request not found: %w", err)
	}
	if diffSide == "" {
		diffSide = "right"
	}
	isSuggestion, suggestionBody := parseSuggestion(body)
	c := &model.PullLineComment{
		PullID:         pr.ID,
		RepoID:         repo.ID,
		AuthorID:       authorID,
		AuthorName:     authorName,
		Path:           path,
		DiffSide:       diffSide,
		Line:           line,
		Body:           body,
		IsSuggestion:   isSuggestion,
		SuggestionBody: suggestionBody,
	}
	if err := s.comments.Create(ctx, c); err != nil {
		return nil, fmt.Errorf("create line comment: %w", err)
	}
	return c, nil
}

// parseSuggestion detects a ```suggestion fenced block in the body and extracts its content.
func parseSuggestion(body string) (isSuggestion bool, suggestionBody string) {
	const open = "```suggestion\n"
	const close = "\n```"
	start := strings.Index(body, open)
	if start == -1 {
		return false, ""
	}
	content := body[start+len(open):]
	end := strings.Index(content, close)
	if end == -1 {
		return false, ""
	}
	return true, content[:end]
}

func (s *PullLineCommentService) ListByPull(ctx context.Context, owner, repoName string, pullNumber int) ([]model.PullLineComment, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return nil, fmt.Errorf("pull request not found: %w", err)
	}
	return s.comments.ListByPull(ctx, pr.ID)
}

// Delete removes a line comment. The caller is responsible for verifying the user
// is either the comment author or has write access to the repo.
func (s *PullLineCommentService) Delete(ctx context.Context, owner, repoName string, id int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.comments.Delete(ctx, id, repo.ID)
}

// GetComment returns the raw line comment by ID (for permission checks in handlers).
func (s *PullLineCommentService) GetComment(ctx context.Context, id int64) (*model.PullLineComment, error) {
	return s.comments.GetByID(ctx, id)
}

// Update edits a line comment body. Only the author may edit.
func (s *PullLineCommentService) Update(ctx context.Context, id, callerID int64, body string) (*model.PullLineComment, error) {
	c, err := s.comments.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("line comment not found: %w", err)
	}
	if c.AuthorID != callerID {
		return nil, fmt.Errorf("forbidden")
	}
	return s.comments.Update(ctx, id, body)
}
