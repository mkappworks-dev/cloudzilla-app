package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/seed"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func seedCmd() *cobra.Command {
	var opts seed.Options
	c := &cobra.Command{
		Use:   "seed",
		Short: "Fill a fresh instance with test data",
		Long: "Creates a superadmin, users, organizations, repositories with a year of backdated git history,\n" +
			"issues, pull requests, discussions, releases, stars and gists. Refuses to run unless the database\n" +
			"has no accounts and git.repos_root is empty. Every account signs in with the --password value.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			// The seed sends thousands of notifications; none may reach a real inbox.
			cfg.SMTP = config.SMTPConfig{}
			database, err := db.Connect(cfg.Database)
			if err != nil {
				return err
			}
			defer func() { _ = database.Close() }()

			opts.Log = cmd.OutOrStdout()
			rep, err := seed.Run(context.Background(), service.New(store.New(database), cfg), cfg.Git.ReposRoot, opts)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n%d users, %d orgs, %d repos (%d commits), %d issues, %d pull requests,\n"+
				"%d comments, %d discussions, %d releases, %d stars, %d gists\n\n"+
				"Sign in as %s (username %s) with the --password value.\n",
				rep.Users, rep.Orgs, rep.Repos, rep.Commits, rep.Issues, rep.Pulls,
				rep.Comments, rep.Discussions, rep.Releases, rep.Stars, rep.Gists,
				seed.AdminEmail, seed.AdminUsername)
			return nil
		},
	}
	c.Flags().IntVar(&opts.Users, "users", 100, "users to create, besides the superadmin")
	c.Flags().IntVar(&opts.Orgs, "orgs", 10, "organizations to create")
	c.Flags().IntVar(&opts.Repos, "repos", 150, "repositories to create")
	c.Flags().Uint64Var(&opts.Seed, "seed", 1, "random seed; the same seed builds the same world")
	c.Flags().StringVar(&opts.Password, "password", seed.DefaultPassword, "password for every seeded account")
	return c
}
