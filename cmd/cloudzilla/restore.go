package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/backup"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func restoreCmd() *cobra.Command {
	var input, pgRestore string
	var replaceHostKey bool
	c := &cobra.Command{
		Use:   "restore",
		Short: "Rebuild an empty instance from a backup archive",
		Long: "Restores into a new, empty database and an empty (or missing) git.repos_root. It refuses to start when the\n" +
			"database already has tables, when git.repos_root is not empty, when the archive's format version is\n" +
			"unknown, or when the backup holds a migration this binary does not have. The whole archive is checked\n" +
			"before anything is written. Migrations newer than the backup are applied afterwards.\n\n" +
			"Sign-ins survive only with the same auth.jwt_secret. Use --input - for stdin.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			database, err := db.Connect(cfg.Database)
			if err != nil {
				return err
			}
			defer func() { _ = database.Close() }()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			stores := store.New(database)
			opts := backup.RestoreOptions{
				DB: database, DSN: cfg.Database.DSN, PgRestore: pgRestore,
				ReposRoot: cfg.Git.ReposRoot, HostKeyPath: cfg.Git.SSHHostKey, ReplaceHostKey: replaceHostKey,
				Log: cmd.ErrOrStderr(),
				ListRepos: func(ctx context.Context) ([]backup.RepoRef, error) {
					repos, err := stores.Repo.ListAll(ctx)
					if err != nil {
						return nil, err
					}
					refs := make([]backup.RepoRef, len(repos))
					for i, r := range repos {
						refs[i] = backup.RepoRef{Owner: r.OwnerName, Name: r.Name}
					}
					return refs, nil
				},
			}
			if cfg.Storage.Backend != "s3" {
				opts.StorageRoot = cfg.Storage.Local.Root
			}
			rep, err := backup.Restore(ctx, input, cmd.InOrStdin(), opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "restored a backup taken %s by Cloudzilla %s (migration %s)\n",
				rep.Manifest.CreatedAt.Format("2006-01-02 15:04 MST"), rep.Manifest.CloudzillaVersion, rep.Manifest.Migration)
			for _, w := range rep.Warnings {
				_, _ = fmt.Fprintf(out, "warning: %s\n", w)
			}
			for _, r := range rep.MissingDirs {
				_, _ = fmt.Fprintf(out, "warning: repository %s has a database row but no directory\n", r)
			}
			for _, d := range rep.OrphanDirs {
				_, _ = fmt.Fprintf(out, "warning: directory %s has no database row\n", d)
			}
			if len(rep.MissingDirs)+len(rep.OrphanDirs) == 0 {
				_, _ = fmt.Fprintln(out, "repositories and database rows agree")
			}
			return nil
		},
	}
	c.Flags().StringVar(&input, "input", "", "archive path, or - for stdin (required)")
	c.Flags().StringVar(&pgRestore, "pg-restore", "pg_restore", "pg_restore binary")
	c.Flags().BoolVar(&replaceHostKey, "replace-host-key", false, "overwrite a different SSH host key already in place")
	_ = c.MarkFlagRequired("input")
	return c
}
