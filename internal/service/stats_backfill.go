package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type BackfillReport struct {
	Repo                 string
	CommitsWalked        int
	CommitsWithKnownUser int
	WeekCommitsBefore    int
	WeekCommitsAfter     int
	Applied              bool
}

// With apply=false this only reports drift; with apply=true it rewrites the repo's stats from git, which is the source of truth.
func BackfillRepoStats(ctx context.Context, stats *store.ContributorStatsStore, code *CodeService, users *store.UserStore, repo model.Repository, apply bool) (BackfillReport, error) {
	walked, err := code.WalkAllRefCommits(repo.OwnerName, repo.Name)
	if err != nil {
		return BackfillReport{}, err
	}

	emailToUser := make(map[string]int64)
	var rows []store.CommitIngestRow
	for _, c := range walked {
		uid, resolved := emailToUser[c.AuthorEmail]
		if !resolved {
			u, err := users.GetByEmail(ctx, c.AuthorEmail)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return BackfillReport{}, err
			}
			if u != nil {
				uid = u.ID
			}
			emailToUser[c.AuthorEmail] = uid
		}
		if uid == 0 {
			continue
		}
		rows = append(rows, store.CommitIngestRow{
			UserID: uid, SHA: c.SHA, When: c.When, Additions: c.Additions, Deletions: c.Deletions,
		})
	}

	before, err := stats.ListForRepo(ctx, repo.ID)
	if err != nil {
		return BackfillReport{}, err
	}
	weekCommitsBefore := 0
	for _, r := range before {
		weekCommitsBefore += r.Commits
	}

	report := BackfillReport{
		Repo:                 repo.OwnerName + "/" + repo.Name,
		CommitsWalked:        len(walked),
		CommitsWithKnownUser: len(rows),
		WeekCommitsBefore:    weekCommitsBefore,
		// Walked SHAs are already unique, so each known-author commit adds exactly one.
		WeekCommitsAfter: len(rows),
		Applied:          apply,
	}
	if apply {
		if err := stats.RebuildRepoStats(ctx, repo.ID, rows); err != nil {
			return BackfillReport{}, err
		}
	}
	return report, nil
}
