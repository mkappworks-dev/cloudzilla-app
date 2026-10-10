package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func (s *RepoService) TopContributors(ctx context.Context, owner, name, ref string, limit int) ([]ContributorStat, error) {
	if s.code == nil {
		return nil, nil
	}
	all, err := s.code.GetContributors(owner, name, ref)
	if err != nil {
		return nil, err
	}
	if all, err = s.mergeContributorsByUser(ctx, all); err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Git groups contributors by author email, so a user's pushed commits and their
// noreply-authored web commits arrive as separate entries. Merged entries take the
// username as Name because the sidebar links each avatar to "/"+Name.
func (s *RepoService) mergeContributorsByUser(ctx context.Context, stats []ContributorStat) ([]ContributorStat, error) {
	merged := make([]ContributorStat, 0, len(stats))
	indexByUser := make(map[int64]int)
	for _, c := range stats {
		u, err := userByAuthorEmail(ctx, s.users, c.Email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if u == nil {
			merged = append(merged, c)
			continue
		}
		if i, ok := indexByUser[u.ID]; ok {
			merged[i].Commits += c.Commits
			merged[i].Additions += c.Additions
			merged[i].Deletions += c.Deletions
			continue
		}
		c.Name = u.Username
		indexByUser[u.ID] = len(merged)
		merged = append(merged, c)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Commits > merged[j].Commits })
	return merged, nil
}

type postReceiveCommit struct {
	AuthorEmail string
	AuthorTime  time.Time
	SHA         string
}

// Dedupes commits shared across multiple updated branches; returns an aggregate error only when every walk fails so a stuck repo doesn't go silent.
func (s *RepoService) OnPostReceive(ctx context.Context, repo *model.Repository, gitRepo *gogit.Repository, commands []*packp.Command) error {
	s.quota.Recompute(repo)
	if s.contributorStats == nil || gitRepo == nil || repo == nil {
		return nil
	}
	seen := make(map[plumbing.Hash]struct{})
	var commits []postReceiveCommit
	var walkAttempts, walkFailures int
	for _, cmd := range commands {
		if cmd == nil {
			continue
		}
		if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
			continue
		}
		if cmd.Action() == packp.Delete {
			continue
		}
		walkAttempts++
		walkErr := forEachPushedCommit(gitRepo, cmd, func(c *object.Commit) error {
			if _, dup := seen[c.Hash]; dup {
				return nil
			}
			seen[c.Hash] = struct{}{}
			commits = append(commits, postReceiveCommit{
				AuthorEmail: c.Author.Email,
				AuthorTime:  c.Author.When,
				SHA:         c.Hash.String(),
			})
			return nil
		})
		if walkErr != nil {
			walkFailures++
			slog.Warn("post-receive: commit walk failed",
				"repo_id", repo.ID, "ref", cmd.Name.String(), "new", cmd.New.String(), "error", walkErr)
		}
	}
	if walkAttempts > 0 && walkFailures == walkAttempts {
		return fmt.Errorf("commit stats: all %d branch walks failed (repo_id=%d)", walkAttempts, repo.ID)
	}
	if len(commits) == 0 {
		return nil
	}

	if s.contributorStats != nil && s.code != nil && repo.OwnerName != "" {
		for _, c := range commits {
			user, err := userByAuthorEmail(ctx, s.users, c.AuthorEmail)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				slog.Warn("post-receive: contributor lookup by email failed",
					"repo_id", repo.ID, "sha", c.SHA, "email", c.AuthorEmail, "error", err)
				continue
			}
			if user == nil {
				continue
			}
			detail, err := s.code.GetCommit(repo.OwnerName, repo.Name, c.SHA)
			if err != nil {
				slog.Warn("post-receive: load commit detail for contributor stats failed",
					"repo_id", repo.ID, "sha", c.SHA, "error", err)
				continue
			}
			if err := s.contributorStats.IngestCommit(ctx, repo.ID, user.ID, c.AuthorTime, c.SHA,
				detail.TotalAdded, detail.TotalDeleted); err != nil {
				slog.Warn("post-receive: contributor stats ingest failed",
					"repo_id", repo.ID, "sha", c.SHA, "user_id", user.ID, "error", err)
			}
		}
	}

	if s.pulls != nil {
		for _, cmd := range commands {
			if cmd == nil || !strings.HasPrefix(cmd.Name.String(), "refs/heads/") || cmd.Action() == packp.Delete {
				continue
			}
			branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
			if err := s.pulls.UpdateHeadSHAByBranch(ctx, repo.ID, branch, cmd.New.String()); err != nil {
				slog.Warn("post-receive: update PR head sha failed",
					"repo_id", repo.ID, "branch", branch, "error", err)
			}
		}
	}

	if s.language != nil && repo.OwnerName != "" && repo.DefaultBranch != "" {
		top, err := s.language.TopLanguageFor(ctx, repo, repo.DefaultBranch)
		if err != nil {
			slog.Warn("post-receive: language composition failed", "repo_id", repo.ID, "error", err)
		} else if err := s.repos.UpdatePrimaryLanguage(ctx, repo.ID, top); err != nil {
			slog.Error("post-receive: update primary language failed", "repo_id", repo.ID, "error", err)
		}
	}

	return nil
}

const (
	pushSummaryCommitCap = 3  // commits listed per push activity row
	pushSummaryWalkCap   = 50 // bounds the walk so a new-branch push doesn't count all of history
)

// PushSummaries walks the commits introduced by each updated branch and returns
// one summary per branch, for recording push activity-feed events.
func (s *RepoService) PushSummaries(gitRepo *gogit.Repository, commands []*packp.Command) []model.PushSummary {
	if gitRepo == nil {
		return nil
	}
	var summaries []model.PushSummary
	for _, cmd := range commands {
		if cmd == nil || !strings.HasPrefix(cmd.Name.String(), "refs/heads/") || cmd.Action() == packp.Delete {
			continue
		}
		var commits []model.CommitSummary
		total := 0
		newBranch := cmd.Action() == packp.Create
		walkErr := forEachPushedCommit(gitRepo, cmd, func(c *object.Commit) error {
			if newBranch && total >= pushSummaryWalkCap {
				return storer.ErrStop
			}
			total++
			if len(commits) < pushSummaryCommitCap {
				commits = append(commits, model.CommitSummary{
					SHA:     c.Hash.String()[:7],
					Message: commitSubject(c.Message),
				})
			}
			return nil
		})
		if walkErr != nil {
			slog.Warn("push summary: commit walk failed", "ref", cmd.Name.String(), "error", walkErr)
		}
		if total == 0 {
			continue
		}
		summaries = append(summaries, model.PushSummary{
			Branch:      strings.TrimPrefix(cmd.Name.String(), "refs/heads/"),
			CommitTotal: total,
			Commits:     commits,
		})
	}
	return summaries
}

// forEachPushedCommit calls fn for each commit cmd adds to its branch until fn
// returns storer.ErrStop. A new branch has no old tip to stop at, so fn sees
// all of its history.
func forEachPushedCommit(gitRepo *gogit.Repository, cmd *packp.Command, fn func(*object.Commit) error) error {
	if cmd.Action() == packp.Create {
		iter, err := gitRepo.Log(&gogit.LogOptions{From: cmd.New})
		if err != nil {
			return err
		}
		defer iter.Close()
		return iter.ForEach(fn)
	}
	commits, err := commitRange(gitRepo, cmd.Old, cmd.New)
	if err != nil {
		return err
	}
	for _, c := range commits {
		err := fn(c)
		if errors.Is(err, storer.ErrStop) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func commitSubject(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return strings.TrimSpace(line)
}
