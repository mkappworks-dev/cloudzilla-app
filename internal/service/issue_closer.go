package service

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type closingRefResolver struct {
	repos   *store.RepoStore
	issues  *store.IssueStore
	repoSvc *RepoService
}

// resolve returns the issues refs name that viewerID can see, once each, in
// repos targets cover when targets are set. A bare "#N" names an issue in
// contextRepo. A reference viewerID can't follow is dropped, so whether its
// repo or issue exists isn't revealed.
func (r closingRefResolver) resolve(ctx context.Context, refs []ClosingRef, contextRepo *model.Repository, viewerID int64, targets []string) []model.Issue {
	var out []model.Issue
	for _, ref := range refs {
		repo := contextRepo
		if ref.Owner != "" && (!strings.EqualFold(ref.Owner, contextRepo.OwnerName) || !strings.EqualFold(ref.Repo, contextRepo.Name)) {
			other, err := r.repos.GetByOwnerName(ctx, ref.Owner, ref.Repo)
			if err != nil {
				continue
			}
			repo = other
		}
		if len(targets) > 0 && !model.TargetsCover(targets, repo.OwnerName, repo.Name) || !r.repoSvc.CanRead(ctx, repo, &viewerID) {
			continue
		}
		issue, err := r.issues.GetByNumber(ctx, repo.ID, ref.Number, &viewerID)
		if err != nil {
			continue
		}
		if !slices.ContainsFunc(out, func(i model.Issue) bool { return i.ID == issue.ID }) {
			out = append(out, *issue)
		}
	}
	return out
}

// CloseActor is who an automatic close acts as. Targets, when set, are the
// repositories and organizations the actor's token is limited to.
type CloseActor struct {
	UserID   int64
	Username string
	Targets  []string
}

// IssueCloser closes issues that a merged PR or a pushed commit closes, with the
// same side effects as a manual close.
type IssueCloser struct {
	resolver closingRefResolver
	issues   *store.IssueStore
	events   *store.IssueEventStore
	repos    *store.RepoStore
	repoSvc  *RepoService
	webhook  *WebhookService
	notif    *NotificationService
	activity *EventService
}

func NewIssueCloser(issues *store.IssueStore, events *store.IssueEventStore, repos *store.RepoStore, repoSvc *RepoService,
	webhook *WebhookService, notif *NotificationService, activity *EventService) *IssueCloser {
	return &IssueCloser{
		resolver: closingRefResolver{repos: repos, issues: issues, repoSvc: repoSvc},
		issues:   issues, events: events, repos: repos, repoSvc: repoSvc,
		webhook: webhook, notif: notif, activity: activity,
	}
}

// CloseForPull closes the issues pr closes on merging into repo's default
// branch: every linked issue and those commitRefs, the closing references in
// the merged commits, name. It does nothing for a merge into another branch.
func (c *IssueCloser) CloseForPull(ctx context.Context, actor CloseActor, repo *model.Repository, pr *model.PullRequest, commitRefs []ClosingRef) {
	if pr.BaseBranch != repo.DefaultBranch {
		return
	}
	ids, err := c.issues.LinkedIssueIDs(ctx, pr.ID)
	if err != nil {
		slog.Error("close issues for pull: list linked issues failed", "pull_id", pr.ID, "error", err)
	}
	for _, issue := range c.resolver.resolve(ctx, commitRefs, repo, actor.UserID, actor.Targets) {
		if !slices.Contains(ids, issue.ID) {
			ids = append(ids, issue.ID)
		}
	}
	pullID, repoID := pr.ID, repo.ID
	for _, id := range ids {
		c.close(ctx, actor, id, model.IssueEvent{PullID: &pullID, SourceRepoID: &repoID})
	}
}

// CloseForPush closes the issues named in the commits a push fast-forwarded the
// default branch by, oldest first. Branch creation and force-pushes would replay
// old or foreign history, so they close nothing.
func (c *IssueCloser) CloseForPush(ctx context.Context, actor CloseActor, repo *model.Repository, gitRepo *gogit.Repository, commands []*packp.Command) {
	for _, cmd := range commands {
		if cmd == nil || cmd.Name.String() != "refs/heads/"+repo.DefaultBranch || cmd.Action() != packp.Update {
			continue
		}
		commits, err := fastForwardCommits(gitRepo, cmd)
		if err != nil {
			slog.Warn("close issues for push: commit walk failed", "repo_id", repo.ID, "error", err)
			continue
		}
		repoID := repo.ID
		for _, commit := range commits {
			for _, issue := range c.resolver.resolve(ctx, ParseClosingRefs(commit.Message), repo, actor.UserID, actor.Targets) {
				c.close(ctx, actor, issue.ID, model.IssueEvent{CommitSHA: commit.Hash.String(), SourceRepoID: &repoID})
			}
		}
	}
}

// fastForwardCommits returns the commits cmd adds, oldest first, or none when
// cmd isn't a fast-forward.
func fastForwardCommits(gitRepo *gogit.Repository, cmd *packp.Command) ([]*object.Commit, error) {
	oldCommit, err := gitRepo.CommitObject(cmd.Old)
	if err != nil {
		return nil, err
	}
	newCommit, err := gitRepo.CommitObject(cmd.New)
	if err != nil {
		return nil, err
	}
	if ff, err := isAncestor(gitRepo, oldCommit, newCommit); err != nil || !ff {
		return nil, err
	}
	commits, err := commitRange(gitRepo, cmd.Old, cmd.New)
	if err != nil {
		return nil, err
	}
	slices.Reverse(commits)
	return commits, nil
}

// close closes issueID as actor, recording ev as its closed event, when actor
// may and the issue is open and not already closed once by ev's PR or commit.
func (c *IssueCloser) close(ctx context.Context, actor CloseActor, issueID int64, ev model.IssueEvent) {
	issue, err := c.issues.GetByID(ctx, issueID)
	if err != nil {
		slog.Warn("auto-close: load issue failed", "issue_id", issueID, "error", err)
		return
	}
	repo, err := c.repos.GetByID(ctx, issue.RepoID)
	if err != nil {
		return
	}
	switch {
	case repo.IsArchived:
		slog.Debug("auto-close skipped: repo archived", "issue_id", issueID, "repo_id", repo.ID)
		return
	case len(actor.Targets) > 0 && !model.TargetsCover(actor.Targets, repo.OwnerName, repo.Name):
		slog.Debug("auto-close skipped: token targets don't cover the repo", "issue_id", issueID, "repo_id", repo.ID, "user_id", actor.UserID)
		return
	case !c.repoSvc.CanWrite(ctx, repo, actor.UserID):
		slog.Debug("auto-close skipped: actor can't write the repo", "issue_id", issueID, "repo_id", repo.ID, "user_id", actor.UserID)
		return
	}
	ev.IssueID, ev.ActorID, ev.ActorName = issueID, actor.UserID, actor.Username
	closed, err := c.events.ClaimClose(ctx, &ev)
	if err != nil {
		slog.Error("auto-close: close failed", "issue_id", issueID, "error", err)
		return
	}
	if !closed {
		return
	}
	issue.State, issue.ClosedAt, issue.UpdatedAt = model.IssueStateClosed, &ev.CreatedAt, ev.CreatedAt

	payload := c.webhook.IssuePayload("closed", *repo, *issue)
	concurrency.Go("webhook.dispatch.issues", func() { c.webhook.Dispatch(repo.ID, "issues", payload) })
	c.notif.NotifyIssueStateChange(ctx, *repo, *issue, actor.UserID, actor.Username)
	repoID := repo.ID
	c.activity.Record(ctx, actor.UserID, actor.Username, &repoID, repo.Name, repo.OwnerName, model.EventIssueClosed, map[string]any{"number": issue.Number})
}
