package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const afterPushTimeout = 5 * time.Minute

// PushService runs what follows a ref update that reached a branch, whether by
// git push or by a commit made in the browser.
type PushService struct {
	repos   *RepoService
	code    *CodeService
	webhook *WebhookService
	events  *EventService
	closer  *IssueCloser
	index   *IndexService
	deps    *DependencyService
	secrets *PushScanService
}

func NewPushService(repos *RepoService, code *CodeService, webhook *WebhookService, events *EventService, closer *IssueCloser, index *IndexService, deps *DependencyService) *PushService {
	return &PushService{repos: repos, code: code, webhook: webhook, events: events, closer: closer, index: index, deps: deps}
}

// WithSecretScan enables the scan after the push; it also covers AfterWebCommit.
func (s *PushService) WithSecretScan(scan *PushScanService) *PushService {
	s.secrets = scan
	return s
}

// AfterPush fires the push webhooks and records activity, closes issues named by
// pushed commits, ingests stats, re-indexes code and parses dependencies, each in
// the background. commands must be only the refs that applied. An actor with no
// Username (a deploy key) records no activity and closes no issues. scan is the push's secret scan, or nil when
// there is none to finish.
func (s *PushService) AfterPush(repo *model.Repository, gitRepo *gogit.Repository, actor CloseActor, commands []*packp.Command, scan *PushScan) {
	if scan != nil {
		concurrency.Go("secret_scan.post_receive", func() {
			ctx, cancel := context.WithTimeout(context.Background(), afterPushTimeout)
			defer cancel()
			scan.PostReceive(ctx, commands)
		})
	}

	for _, cmd := range commands {
		if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") || cmd.Action() == packp.Delete {
			continue
		}
		branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
		payload := s.webhook.PushPayload(*repo, actor.Username, branch, cmd.New.String())
		repoID := repo.ID
		concurrency.Go("webhook.dispatch.push", func() {
			s.webhook.Dispatch(repoID, "push", payload)
		})
	}

	if actor.Username != "" {
		repoID := repo.ID
		repoName, ownerName := repo.Name, repo.OwnerName
		concurrency.Go("event.record.push", func() {
			for _, ps := range s.repos.PushSummaries(gitRepo, commands) {
				s.events.RecordPush(context.Background(), actor.UserID, actor.Username, &repoID, repoName, ownerName, ps)
			}
		})
		concurrency.Go("issue_closer.close_for_push", func() {
			ctx, cancel := context.WithTimeout(context.Background(), afterPushTimeout)
			defer cancel()
			s.closer.CloseForPush(ctx, actor, repo, gitRepo, commands)
		})
	}

	concurrency.Go("repo.on_post_receive", func() {
		ctx, cancel := context.WithTimeout(context.Background(), afterPushTimeout)
		defer cancel()
		if err := s.repos.OnPostReceive(ctx, repo, gitRepo, commands); err != nil {
			slog.Error("post-receive: commit stats ingest failed",
				"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		}
	})

	concurrency.Go("index.index_repo", func() {
		ctx, cancel := context.WithTimeout(context.Background(), afterPushTimeout)
		defer cancel()
		if err := s.index.IndexRepo(ctx, repo); err != nil {
			slog.Error("index: failed to re-index repo",
				"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		}
	})

	concurrency.Go("dependency.parse_and_store", func() {
		ctx, cancel := context.WithTimeout(context.Background(), afterPushTimeout)
		defer cancel()
		if err := s.deps.ParseAndStore(ctx, repo); err != nil {
			slog.Error("dependency: failed to parse and store manifests",
				"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		}
	})
}

// AfterWebCommit runs AfterPush for the branch a commit made in the browser moved.
func (s *PushService) AfterWebCommit(repo *model.Repository, actor CloseActor, upd RefUpdate) {
	gitRepo, err := s.code.openRepo(repo.OwnerName, repo.Name)
	if err != nil {
		slog.Error("web commit: open repo for push side effects failed",
			"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		return
	}
	var scan *PushScan
	if s.secrets != nil {
		scan = s.secrets.ForCommit(repo, gitRepo, actor.UserID, upd.New)
	}
	s.AfterPush(repo, gitRepo, actor, []*packp.Command{{
		Name: plumbing.NewBranchReferenceName(upd.Branch),
		Old:  upd.Old,
		New:  upd.New,
	}}, scan)
}
