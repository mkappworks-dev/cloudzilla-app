package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrNotConvertible: only a titled card with no issue or pull link can become an issue.
var ErrNotConvertible = errors.New("only a note card without a link can be converted to an issue")

// WithConvertDeps enables ConvertCardToIssue.
func (s *ProjectService) WithConvertDeps(issues *IssueService, issueStore *store.IssueStore, labels *store.LabelStore, assignees *store.AssigneeStore) *ProjectService {
	s.issues, s.issueStore, s.labelStore, s.assigneeStore = issues, issueStore, labels, assignees
	return s
}

// ConvertCardToIssue creates a public issue from a note card and links the card to it. The
// issue takes over the card's description, labels and assignees; the due date stays on the card.
// The returned issue lets the caller announce it like any other newly opened issue.
func (s *ProjectService) ConvertCardToIssue(ctx context.Context, projectID, cardID, userID int64) (*model.ProjectCard, *model.Issue, error) {
	repo, err := s.repoForProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	if !s.repos.CanWrite(ctx, repo, userID) {
		return nil, nil, ErrForbidden
	}
	card, err := s.projects.GetCardInProject(ctx, cardID, projectID)
	if errors.Is(err, store.ErrCardNotInProject) {
		return nil, nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(card.Title) == "" || card.IssueID != nil || card.PullID != nil {
		return nil, nil, ErrNotConvertible
	}
	assignees, err := s.projects.CardAssignees(ctx, []int64{cardID})
	if err != nil {
		return nil, nil, err
	}
	labels, err := s.projects.CardLabels(ctx, []int64{cardID})
	if err != nil {
		return nil, nil, err
	}

	issue, err := s.createIssue(ctx, repo, userID, card)
	if err != nil {
		return nil, nil, err
	}
	if err := s.linkConverted(ctx, projectID, card, issue.ID, assignees[cardID], labels[cardID]); err != nil {
		if errors.Is(err, store.ErrLinkCommit) {
			// The commit may have succeeded; deleting the issue would then cascade-delete the linked card.
			slog.Error("convert card: link commit failed, issue kept", "card_id", cardID, "issue_id", issue.ID, "error", err)
			return nil, nil, err
		}
		// Detached so a cancelled request doesn't strand the orphan issue.
		if delErr := s.issueStore.DeleteByID(context.WithoutCancel(ctx), issue.ID); delErr != nil {
			slog.Error("convert card: issue orphaned, delete failed", "card_id", cardID, "issue_id", issue.ID, "error", delErr)
		}
		return nil, nil, err
	}
	if err := s.projects.TouchProject(ctx, projectID); err != nil {
		slog.Warn("convert card: touch project failed", "project_id", projectID, "error", err)
	}
	converted, err := s.projects.GetCardInProject(ctx, cardID, projectID)
	if err != nil {
		return nil, nil, err
	}
	return converted, issue, nil
}

const issueCreateAttempts = 3

// IssueStore.Create numbers issues with MAX(number)+1, so a concurrent creator in the
// same repo can take the number first; the unique violation is retried with a fresh one.
func (s *ProjectService) createIssue(ctx context.Context, repo *model.Repository, userID int64, card *model.ProjectCard) (*model.Issue, error) {
	var err error
	for range issueCreateAttempts {
		var issue *model.Issue
		issue, err = s.issues.Create(ctx, repo.OwnerName, repo.Name, userID, card.Title, card.Note, "public")
		if !isUniqueViolation(err) {
			return issue, err
		}
	}
	return nil, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *ProjectService) linkConverted(ctx context.Context, projectID int64, card *model.ProjectCard, issueID int64, assignees []model.CardUser, labels []model.Label) error {
	for _, l := range labels {
		if err := s.labelStore.AddToIssue(ctx, issueID, l.ID); err != nil {
			return err
		}
	}
	for _, a := range assignees {
		if err := s.assigneeStore.AddToIssue(ctx, issueID, a.ID); err != nil {
			return err
		}
	}
	err := s.projects.LinkNoteCardToIssue(ctx, card.ID, projectID, issueID)
	if errors.Is(err, store.ErrCardNotLinkable) {
		return ErrNotConvertible
	}
	return err
}
