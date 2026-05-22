package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gitgc"
	"github.com/spf13/cobra"
)

func gcCmd() *cobra.Command {
	var grace time.Duration
	var dryRun bool
	var repoArg string

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Prune unreferenced loose objects from repositories",
		Long: "Removes loose objects that are unreachable from every ref and older than\n" +
			"the grace period. Runs across all repositories under git.repos_root, or a\n" +
			"single one with --repo. Safe to run on a schedule (e.g. cron).",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}

			repoPaths, err := resolveRepos(cfg.Git.ReposRoot, repoArg)
			if err != nil {
				return err
			}

			opts := gitgc.Options{Grace: grace, DryRun: dryRun}
			var totalPruned, failures int
			var totalReclaimed int64
			for _, p := range repoPaths {
				res, err := gitgc.Prune(p, opts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "gc %s: %v\n", p, err)
					failures++
					continue
				}
				fmt.Printf("%s: scanned=%d pruned=%d kept=%d reclaimed=%s\n",
					res.Repo, res.Scanned, res.Pruned, res.Kept, formatBytes(res.Reclaimed))
				totalPruned += res.Pruned
				totalReclaimed += res.Reclaimed
			}

			verb := "pruned"
			if dryRun {
				verb = "would prune"
			}
			fmt.Printf("total: %s %d objects across %d repositories, %s reclaimed\n",
				verb, totalPruned, len(repoPaths)-failures, formatBytes(totalReclaimed))
			if failures > 0 {
				return fmt.Errorf("%d repositories failed", failures)
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&grace, "grace", 14*24*time.Hour, "minimum age before an unreferenced object is pruned")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be pruned without deleting")
	cmd.Flags().StringVar(&repoArg, "repo", "", "prune a single repository (owner/name); default: all")
	return cmd
}

// resolveRepos returns the bare-repo paths to prune: a single owner/name
// when repoArg is set, otherwise every <owner>/<repo>.git under root.
func resolveRepos(root, repoArg string) ([]string, error) {
	if repoArg != "" {
		parts := strings.Split(repoArg, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("--repo must be in owner/name form, got %q", repoArg)
		}
		return []string{filepath.Join(root, parts[0], parts[1]+".git")}, nil
	}

	owners, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read repos root %s: %w", root, err)
	}
	var paths []string
	for _, owner := range owners {
		if !owner.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(root, owner.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", owner.Name(), err)
		}
		for _, repo := range repos {
			if repo.IsDir() && strings.HasSuffix(repo.Name(), ".git") {
				paths = append(paths, filepath.Join(root, owner.Name(), repo.Name()))
			}
		}
	}
	return paths, nil
}

// formatBytes renders a byte count in binary units.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
