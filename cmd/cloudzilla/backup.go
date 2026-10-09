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
)

func backupCmd() *cobra.Command {
	var output, pgDump string
	c := &cobra.Command{
		Use:   "backup",
		Short: "Write the database, repositories and SSH host key to one tar archive",
		Long: "Dumps the database with pg_dump, then copies every repository under git.repos_root (refs before\n" +
			"objects, so a push during the backup still restores to a consistent repository), the local storage\n" +
			"root and the SSH host key into one uncompressed tar with mode 0600. The archive holds password\n" +
			"hashes, secrets and the host key: store it encrypted. Do not run `gc` while it runs.\n\n" +
			"Use --output - for stdout, e.g. to pipe through zstd.",
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

			opts := backup.CreateOptions{
				DB: database, DSN: cfg.Database.DSN, PgDump: pgDump,
				ReposRoot: cfg.Git.ReposRoot, HostKeyPath: cfg.Git.SSHHostKey, Version: version,
			}
			if cfg.Storage.Backend == "s3" {
				opts.ObjectStorage = &backup.ObjectStorage{Backend: "s3", Bucket: cfg.Storage.S3.Bucket}
			} else {
				opts.StorageRoot = cfg.Storage.Local.Root
			}
			m, err := backup.Create(ctx, output, cmd.OutOrStdout(), opts)
			if err != nil {
				return err
			}
			// stdout may be the archive itself.
			dest := cmd.ErrOrStderr()
			_, _ = fmt.Fprintf(dest, "backup written to %s (migration %s, pg_dump %s)\n", output, m.Migration, m.PgDumpVersion)
			for _, name := range []string{backup.SectionDatabase, backup.SectionGitRepos, backup.SectionStorage, backup.SectionHostKey} {
				if s, ok := m.Sections[name]; ok {
					_, _ = fmt.Fprintf(dest, "  %-12s %d files, %s\n", name, s.Files, formatBytes(s.Bytes))
				}
			}
			if o := m.ObjectStorage; o != nil {
				_, _ = fmt.Fprintf(dest, "  %s bucket %q is not in the archive: back it up separately\n", o.Backend, o.Bucket)
			}
			return nil
		},
	}
	c.Flags().StringVar(&output, "output", "", "archive path, or - for stdout (required)")
	c.Flags().StringVar(&pgDump, "pg-dump", "pg_dump", "pg_dump binary (version at least the server's)")
	_ = c.MarkFlagRequired("output")
	return c
}
