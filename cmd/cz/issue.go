package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
)

func issueCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "issue", Short: "Work with issues"}
	cmd.AddCommand(issueListCmd(a), issueViewCmd(a), issueCreateCmd(a), issueCommentCmd(a), issueCloseCmd(a))
	return cmd
}

func parseIssueNumber(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid issue number %q", s)
	}
	return n, nil
}

func issueWebURL(host string, r cli.Repo, number int) string {
	return fmt.Sprintf("%s/%s/issues/%d", host, r, number)
}

func issueListCmd(a *app) *cobra.Command {
	var flag, state string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if state != "" && state != "open" && state != "closed" {
				return errors.New(`--state must be "open" or "closed"`)
			}
			creds, c, err := a.login()
			if err != nil {
				return err
			}
			target, err := a.targetRepo(creds.Host, flag, nil)
			if err != nil {
				return err
			}
			issues, err := c.ListIssues(cmd.Context(), target, state)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				raws := make([]json.RawMessage, len(issues))
				for i, is := range issues {
					raws[i] = is.Raw
				}
				return a.printJSON(raws)
			}
			if len(issues) == 0 {
				_, err := fmt.Fprintln(a.stdout, "No issues")
				return err
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NUMBER\tSTATE\tTITLE\tAUTHOR")
			for _, is := range issues {
				fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\n", is.Number, is.State, shorten(is.Title, 60), is.AuthorName)
			}
			return tw.Flush()
		},
	}
	addRepoFlag(cmd, &flag)
	cmd.Flags().StringVar(&state, "state", "", "only issues in this state: open or closed")
	return cmd
}

func issueViewCmd(a *app) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "view <number>",
		Short: "Show an issue and its comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := parseIssueNumber(args[0])
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
			is, err := c.GetIssue(cmd.Context(), target, number)
			if err != nil {
				return err
			}
			comments, err := c.ListIssueComments(cmd.Context(), target, number)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(is.Raw, &obj); err != nil {
					return err
				}
				raws := make([]json.RawMessage, len(comments))
				for i, cm := range comments {
					raws[i] = cm.Raw
				}
				if obj["comments"], err = json.Marshal(raws); err != nil {
					return err
				}
				return a.printJSON(obj)
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "Issue:\t#%d %s\n", is.Number, is.Title)
			fmt.Fprintf(tw, "State:\t%s\n", is.State)
			fmt.Fprintf(tw, "Author:\t%s\n", is.AuthorName)
			fmt.Fprintf(tw, "Created:\t%s\n", is.CreatedAt.Format("2006-01-02"))
			fmt.Fprintf(tw, "URL:\t%s\n", issueWebURL(creds.Host, target, is.Number))
			if err := tw.Flush(); err != nil {
				return err
			}
			if strings.TrimSpace(is.Body) != "" {
				fmt.Fprintf(a.stdout, "\n%s\n", strings.TrimSpace(is.Body))
			}
			for _, cm := range comments {
				fmt.Fprintf(a.stdout, "\n--- %s commented on %s ---\n%s\n", cm.AuthorName, cm.CreatedAt.Format("2006-01-02"), strings.TrimSpace(cm.Body))
			}
			return nil
		},
	}
	addRepoFlag(cmd, &flag)
	return cmd
}

func issueCreateCmd(a *app) *cobra.Command {
	var flag, title string
	var labels, assignees []string
	var bf bodyFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(title) == "" {
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
			body, err := a.readBody(cmd.Context(), bf, cmd)
			if err != nil {
				return err
			}
			is, err := c.CreateIssueWith(cmd.Context(), target, title, body, labels, assignees)
			if err != nil {
				var partial *cli.PartialIssueError
				if errors.As(err, &partial) {
					return fmt.Errorf("%w (%s)", err, issueWebURL(creds.Host, target, partial.Issue.Number))
				}
				return err
			}
			if a.jsonOut() {
				return a.printJSON(is.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Created #%d\n%s\n", is.Number, issueWebURL(creds.Host, target, is.Number))
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	addBodyFlags(cmd, &bf)
	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "issue title")
	f.StringSliceVarP(&labels, "label", "l", nil, "label name to apply (repeatable)")
	f.StringSliceVarP(&assignees, "assignee", "a", nil, "username to assign (repeatable)")
	return cmd
}

func issueCommentCmd(a *app) *cobra.Command {
	var flag string
	var bf bodyFlags
	cmd := &cobra.Command{
		Use:   "comment <number>",
		Short: "Add a comment to an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := parseIssueNumber(args[0])
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
			body, err := a.readBody(cmd.Context(), bf, cmd)
			if err != nil {
				return err
			}
			if strings.TrimSpace(body) == "" {
				return errors.New("comment body is empty; use --body or --body-file")
			}
			cm, err := c.CommentOnIssue(cmd.Context(), target, number, body)
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(cm.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Commented on #%d\n%s\n", number, issueWebURL(creds.Host, target, number))
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	addBodyFlags(cmd, &bf)
	return cmd
}

func issueCloseCmd(a *app) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "close <number>",
		Short: "Close an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := parseIssueNumber(args[0])
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
			is, err := c.SetIssueState(cmd.Context(), target, number, "closed")
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(is.Raw)
			}
			_, err = fmt.Fprintf(a.stdout, "Closed #%d\n%s\n", number, issueWebURL(creds.Host, target, number))
			return err
		},
	}
	addRepoFlag(cmd, &flag)
	return cmd
}
