package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func statsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stats", Short: "Statistics maintenance commands"}
	cmd.AddCommand(statsBackfillCmd())
	return cmd
}

func statsBackfillCmd() *cobra.Command {
	var repoArg string
	var allRepos bool
	var apply bool

	c := &cobra.Command{
		Use:          "backfill",
		Short:        "Recompute contributor and heatmap stats from git history",
		Long:         "Dry-run by default: reports drift without writing. Pass --apply to rewrite the aggregates.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (repoArg != "") == allRepos {
				return fmt.Errorf("exactly one of --repo or --all is required")
			}
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			database, err := db.Connect(cfg.Database)
			if err != nil {
				return err
			}
			defer func() { _ = database.Close() }()

			stores := store.New(database)
			code := service.NewCodeService(cfg.Git)
			ctx := context.Background()

			var repos []model.Repository
			if allRepos {
				repos, err = stores.Repo.ListAll(ctx)
				if err != nil {
					return err
				}
			} else {
				owner, name, ok := strings.Cut(repoArg, "/")
				if !ok || owner == "" || name == "" {
					return fmt.Errorf("--repo must be owner/name, got %q", repoArg)
				}
				r, err := stores.Repo.GetByOwnerName(ctx, owner, name)
				if err != nil {
					return err
				}
				repos = []model.Repository{*r}
			}

			verb := "would set"
			if apply {
				verb = "set"
			}
			var failures int
			for _, r := range repos {
				report, err := service.BackfillRepoStats(ctx, stores.ContributorStats, code, stores.User, r, apply)
				if errors.Is(err, gogit.ErrRepositoryNotExists) {
					fmt.Printf("%-40s skipped (no git repository on disk)\n", r.OwnerName+"/"+r.Name)
					continue
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "backfill %s/%s: %v\n", r.OwnerName, r.Name, err)
					failures++
					continue
				}
				fmt.Printf("%-40s walked=%d known-user=%d week-commits %d -> %s %d\n",
					report.Repo, report.CommitsWalked, report.CommitsWithKnownUser,
					report.WeekCommitsBefore, verb, report.WeekCommitsAfter)
			}
			if !apply {
				fmt.Println("\n(dry-run — re-run with --apply to write changes)")
			}
			if failures > 0 {
				return fmt.Errorf("%d repositories failed", failures)
			}
			return nil
		},
	}
	c.Flags().StringVar(&repoArg, "repo", "", "repository as owner/name")
	c.Flags().BoolVar(&allRepos, "all", false, "backfill every repository")
	c.Flags().BoolVar(&apply, "apply", false, "write changes (default: dry-run)")
	return c
}
