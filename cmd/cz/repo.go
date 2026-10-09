package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
)

func repoCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "repo", Short: "Work with repositories"}
	cmd.AddCommand(repoListCmd(a), repoViewCmd(a), repoCreateCmd(a), repoForkCmd(a), repoCloneCmd(a))
	return cmd
}

func visibility(private bool) string {
	if private {
		return "private"
	}
	return "public"
}

func repoListCmd(a *app) *cobra.Command {
	var owner string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the repositories you can read",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := a.login()
			if err != nil {
				return err
			}
			repos, err := c.ListRepos(cmd.Context(), owner)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				raws := make([]json.RawMessage, len(repos))
				for i, r := range repos {
					raws[i] = r.Raw
				}
				return a.printJSON(raws)
			}
			if len(repos) == 0 {
				_, err := fmt.Fprintln(a.stdout, "No repositories")
				return err
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tVISIBILITY\tDESCRIPTION")
			for _, r := range repos {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Repo(), visibility(r.Private), shorten(r.Description, 60))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "only repositories of this user or organization")
	return cmd
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func repoViewCmd(a *app) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "view [owner/repo]",
		Short: "Show a repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, args)
			if err != nil {
				return err
			}
			info, err := c.GetRepo(cmd.Context(), target)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(info.Raw)
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintf(tw, "Name:\t%s\n", info.Repo())
			_, _ = fmt.Fprintf(tw, "Description:\t%s\n", info.Description)
			_, _ = fmt.Fprintf(tw, "Visibility:\t%s\n", visibility(info.Private))
			_, _ = fmt.Fprintf(tw, "Default branch:\t%s\n", info.DefaultBranch)
			_, _ = fmt.Fprintf(tw, "Forks:\t%d\n", info.ForkCount)
			_, _ = fmt.Fprintf(tw, "Updated:\t%s\n", info.UpdatedAt)
			_, _ = fmt.Fprintf(tw, "URL:\t%s\n", repoWebURL(creds.Host, info.Repo()))
			return tw.Flush()
		},
	}
	addRepoFlag(cmd, &flag)
	return cmd
}

func repoWebURL(host string, r cli.Repo) string { return host + "/" + r.String() }

func repoCreateCmd(a *app) *cobra.Command {
	var p cli.CreateRepo
	var private bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			p.Name = args[0]
			if cmd.Flags().Changed("private") {
				p.Private = &private
			}
			info, err := c.CreateRepo(cmd.Context(), p)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(info.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Created %s\n%s\n", info.Repo(), repoWebURL(creds.Host, info.Repo()))
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&p.Org, "org", "", "create it in this organization instead of your account")
	f.BoolVar(&private, "private", false, "make it private (an organization's default visibility applies when omitted)")
	f.StringVarP(&p.Description, "description", "d", "", "short description")
	f.BoolVar(&p.AddReadme, "readme", false, "start with a README")
	f.StringVar(&p.Gitignore, "gitignore", "", "start with a .gitignore template, e.g. Go")
	f.StringVar(&p.License, "license", "", "start with a license, e.g. mit")
	return cmd
}

func repoForkCmd(a *app) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "fork [owner/repo]",
		Short: "Fork a repository into your account",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, args)
			if err != nil {
				return err
			}
			forked, raw, err := c.ForkRepo(cmd.Context(), target)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Forked %s to %s\n%s\n", target, forked, repoWebURL(creds.Host, forked))
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	return cmd
}

func repoCloneCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "clone <owner/repo> [directory] [-- git-clone-flags...]",
		Short: "Clone a repository with git, authenticating with your token",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, _, err := a.login()
			if err != nil {
				return err
			}
			repo, err := cli.ParseRepo(args[0], creds.Host)
			if err != nil {
				return err
			}
			rest := args[1:]
			var dest, extra []string
			if dash := cmd.ArgsLenAtDash(); dash >= 0 {
				dest, extra = args[1:dash], args[dash:]
			} else {
				dest = rest
			}
			if len(dest) > 1 {
				return fmt.Errorf("unexpected argument %q", dest[1])
			}
			gitArgs := cloneArgs(creds.Host, repo, dest, extra)
			env := []string{cloneTokenEnv + "=" + creds.Token, "GIT_TERMINAL_PROMPT=0"}
			return a.git(cmd.Context(), env, gitArgs...)
		},
	}
}

const cloneTokenEnv = "CZ_GIT_TOKEN"

// The helper reads the token from the child's environment, so argv and the URL never carry it, and -c ahead of the subcommand keeps it out of the clone's .git/config.
func cloneArgs(host string, repo cli.Repo, dest, extra []string) []string {
	helper := `!f() { test "$1" = get && printf 'username=cz\npassword=%s\n' "$` + cloneTokenEnv + `"; }; f`
	key := "credential." + host + ".helper"
	args := []string{"-c", key + "=", "-c", key + "=" + helper, "clone"}
	args = append(args, extra...)
	args = append(args, host+"/"+repo.String()+".git")
	return append(args, dest...)
}

func (a *app) git(ctx context.Context, env []string, args ...string) error {
	if a.runGit != nil {
		return a.runGit(ctx, env, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = a.stdout, a.stderr
	if err := cmd.Run(); err != nil {
		if _, isExit := err.(*exec.ExitError); isExit {
			return fmt.Errorf("git clone failed: %w", err)
		}
		return fmt.Errorf("running git: %w", err)
	}
	return nil
}
