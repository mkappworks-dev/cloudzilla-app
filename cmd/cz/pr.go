package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
)

func prCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "pr", Short: "Work with pull requests"}
	cmd.AddCommand(prListCmd(a), prViewCmd(a), prCreateCmd(a), prMergeCmd(a), prCloseCmd(a), prReviewCmd(a))
	return cmd
}

func prNumber(arg string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(arg, "#"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q is not a pull request number", arg)
	}
	return n, nil
}

func prWebURL(host string, r cli.Repo, number int) string {
	return host + "/" + r.String() + "/pulls/" + strconv.Itoa(number)
}

func prListCmd(a *app) *cobra.Command {
	var flag, state string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch state {
			case "open", "closed", "merged", "all":
			default:
				return fmt.Errorf("--state must be open, closed, merged or all, not %q", state)
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			pulls, err := c.ListPulls(cmd.Context(), target, state)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				raws := make([]json.RawMessage, len(pulls))
				for i, p := range pulls {
					raws[i] = p.Raw
				}
				return a.printJSON(raws)
			}
			if len(pulls) == 0 {
				_, err := fmt.Fprintln(a.stdout, "No pull requests")
				return err
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NUMBER\tTITLE\tBRANCH\tSTATE\tAUTHOR")
			for _, p := range pulls {
				fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\n", p.Number, shorten(p.Title, 60), p.HeadBranch+" → "+p.BaseBranch, prState(p), p.AuthorName)
			}
			return tw.Flush()
		},
	}
	addRepoFlag(cmd, &flag)
	cmd.Flags().StringVarP(&state, "state", "s", "open", "open, closed, merged or all")
	return cmd
}

func prState(p cli.Pull) string {
	if p.IsDraft && p.State == "open" {
		return "draft"
	}
	return p.State
}

func prViewCmd(a *app) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "view <number>",
		Short: "Show a pull request with its reviews and line comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := prNumber(args[0])
			if err != nil {
				return err
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			pull, err := c.GetPull(ctx, target, n)
			if err != nil {
				return err
			}
			reviews, err := c.ListReviews(ctx, target, n)
			if err != nil {
				return err
			}
			comments, err := c.ListLineComments(ctx, target, n)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(map[string]any{
					"pull": pull.Raw, "reviews": rawReviews(reviews), "line_comments": rawComments(comments),
				})
			}
			return a.printPull(creds.Host, target, pull, reviews, comments)
		},
	}
	addRepoFlag(cmd, &flag)
	return cmd
}

func rawReviews(rs []cli.Review) []json.RawMessage {
	out := make([]json.RawMessage, len(rs))
	for i, r := range rs {
		out[i] = r.Raw
	}
	return out
}

func rawComments(cs []cli.LineComment) []json.RawMessage {
	out := make([]json.RawMessage, len(cs))
	for i, c := range cs {
		out[i] = c.Raw
	}
	return out
}

func (a *app) printPull(host string, repo cli.Repo, p cli.Pull, reviews []cli.Review, comments []cli.LineComment) error {
	w := a.stdout
	fmt.Fprintf(w, "#%d %s\n", p.Number, p.Title)
	fmt.Fprintf(w, "%s · %s wants to merge %s into %s\n", prState(p), p.AuthorName, p.HeadBranch, p.BaseBranch)
	fmt.Fprintln(w, prWebURL(host, repo, p.Number))
	if body := strings.TrimSpace(p.Body); body != "" {
		fmt.Fprintf(w, "\n%s\n", body)
	}
	if len(reviews) > 0 {
		fmt.Fprintln(w, "\nReviews")
		for _, r := range reviews {
			fmt.Fprintf(w, "  %s: %s\n", r.AuthorName, strings.ReplaceAll(r.State, "_", " "))
			if body := strings.TrimSpace(r.Body); body != "" {
				fmt.Fprintf(w, "    %s\n", indent(body, "    "))
			}
		}
	}
	if len(comments) > 0 {
		fmt.Fprintln(w, "\nLine comments")
		for _, c := range comments {
			fmt.Fprintf(w, "  %s:%d  %s\n    %s\n", c.Path, c.Line, c.AuthorName, indent(strings.TrimSpace(c.Body), "    "))
		}
	}
	return nil
}

func indent(s, prefix string) string {
	return strings.ReplaceAll(s, "\n", "\n"+prefix)
}

func prCreateCmd(a *app) *cobra.Command {
	var flag string
	var p cli.CreatePull
	var bf bodyFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a pull request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(p.Title) == "" {
				return errors.New("--title is required")
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if p.Head == "" {
				if p.Head, err = a.currentBranch(); err != nil {
					return err
				}
			}
			if p.Base == "" {
				info, err := c.GetRepo(ctx, target)
				if err != nil {
					return err
				}
				if p.Base = info.DefaultBranch; p.Base == "" {
					return errors.New("the repository has no default branch yet; pass --base")
				}
			}
			if p.Head == p.Base {
				return fmt.Errorf("head and base are both %q; pass --head or --base", p.Head)
			}
			if err := a.requirePushed(ctx, creds, target, p.Head); err != nil {
				return err
			}
			if p.Body, err = a.readBody(ctx, bf, cmd); err != nil {
				return err
			}
			pull, err := c.CreatePull(ctx, target, p)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(pull.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Created #%d %s\n%s\n", pull.Number, pull.Title, prWebURL(creds.Host, target, pull.Number))
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	addBodyFlags(cmd, &bf)
	f := cmd.Flags()
	f.StringVarP(&p.Title, "title", "t", "", "title (required)")
	f.StringVarP(&p.Head, "head", "H", "", "branch with the changes (default: the current branch)")
	f.StringVarP(&p.Base, "base", "B", "", "branch to merge into (default: the repository's default branch)")
	f.BoolVarP(&p.Draft, "draft", "d", false, "open as a draft")
	return cmd
}

func (a *app) currentBranch() (string, error) {
	getwd := a.getwd
	if getwd == nil {
		getwd = os.Getwd
	}
	dir, err := getwd()
	if err != nil {
		return "", err
	}
	return cli.CurrentBranch(dir)
}

// The server creates a pull request for any branch name, even one that doesn't exist, so the check has to happen here.
func (a *app) requirePushed(ctx context.Context, creds cli.Credentials, repo cli.Repo, head string) error {
	// The first four args are the credential-helper config cloneArgs builds; they keep the token out of argv.
	args := append(cloneArgs(creds.Host, repo, nil, nil)[:4:4], "ls-remote", "--exit-code", "--heads", creds.Host+"/"+repo.String()+".git", head)
	env := []string{cloneTokenEnv + "=" + creds.Token, "GIT_TERMINAL_PROMPT=0"}
	err := a.lsRemote(ctx, env, args)
	var exit interface{ ExitCode() int }
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() == 2:
		return fmt.Errorf("branch %q is not on the remote; push it first with `git push -u origin %s`", head, head)
	}
	return fmt.Errorf("could not check that branch %q exists on %s: %w", head, creds.Host, err)
}

func (a *app) lsRemote(ctx context.Context, env, args []string) error {
	if a.runGit != nil {
		return a.runGit(ctx, env, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run()
}

func prMergeCmd(a *app) *cobra.Command {
	var flag string
	var ff, merge, squash bool
	cmd := &cobra.Command{
		Use:   "merge <number>",
		Short: "Merge a pull request (needs a token with repo:write)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := prNumber(args[0])
			if err != nil {
				return err
			}
			strategy := "ff"
			switch {
			case merge:
				strategy = "merge"
			case squash:
				strategy = "squash"
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			pull, err := c.MergePull(cmd.Context(), target, n, strategy)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(pull.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Merged #%d into %s (%s)\n", pull.Number, pull.BaseBranch, strategy)
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	f := cmd.Flags()
	f.BoolVar(&ff, "ff", false, "fast-forward the base branch (default)")
	f.BoolVar(&merge, "merge", false, "create a merge commit")
	f.BoolVar(&squash, "squash", false, "squash the commits into one")
	cmd.MarkFlagsMutuallyExclusive("ff", "merge", "squash")
	return cmd
}

func prCloseCmd(a *app) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "close <number>",
		Short: "Close a pull request without merging",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := prNumber(args[0])
			if err != nil {
				return err
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			pull, err := c.ClosePull(cmd.Context(), target, n)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(pull.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Closed #%d\n", pull.Number)
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	return cmd
}

func prReviewCmd(a *app) *cobra.Command {
	var flag string
	var approve, requestChanges, comment bool
	var bf bodyFlags
	cmd := &cobra.Command{
		Use:   "review <number>",
		Short: "Approve, request changes on, or comment on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := prNumber(args[0])
			if err != nil {
				return err
			}
			var state, verb string
			switch {
			case approve:
				state, verb = cli.ReviewApproved, "Approved"
			case requestChanges:
				state, verb = cli.ReviewChangesRequested, "Requested changes on"
			default:
				state, verb = cli.ReviewCommented, "Commented on"
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			body, err := a.readBody(cmd.Context(), bf, cmd)
			if err != nil {
				return err
			}
			if state != cli.ReviewApproved && strings.TrimSpace(body) == "" {
				return errors.New("this review needs a body: pass --body or --body-file")
			}
			review, err := c.SubmitReview(cmd.Context(), target, n, state, body)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(review.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "%s #%d\n", verb, n)
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	addBodyFlags(cmd, &bf)
	f := cmd.Flags()
	f.BoolVar(&approve, "approve", false, "approve the changes")
	f.BoolVar(&requestChanges, "request-changes", false, "block the merge until you change your review")
	f.BoolVar(&comment, "comment", false, "leave a comment without approving or blocking")
	cmd.MarkFlagsMutuallyExclusive("approve", "request-changes", "comment")
	cmd.MarkFlagsOneRequired("approve", "request-changes", "comment")
	return cmd
}
