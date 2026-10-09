package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
)

func authCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Log in and out of a Cloudzilla host"}
	cmd.AddCommand(loginCmd(a), statusCmd(a), logoutCmd(a))
	return cmd
}

func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	return enc.Encode(v)
}

func loginCmd(a *app) *cobra.Command {
	var host string
	var scopes []string
	var withToken, insecure, noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in through the browser, or with a personal access token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host = cli.NormalizeHost(host)
			if host == "" {
				host = cli.NormalizeHost(a.getenv("CZ_HOST"))
			}
			if host == "" {
				return errors.New("no host; pass --host URL")
			}
			var token string
			var err error
			if withToken {
				token, err = a.stdinToken()
			} else {
				token, err = a.deviceLogin(cmd.Context(), host, scopes, noBrowser)
			}
			if err != nil {
				return err
			}
			creds := cli.Credentials{Host: host, Token: token}
			user, err := a.client(creds).CurrentUser(cmd.Context())
			if err != nil {
				return err
			}
			store, err := a.newStore(insecure)
			if err != nil {
				return err
			}
			where, err := store.Save(creds)
			if err != nil {
				return fmt.Errorf("saving login: %w", err)
			}
			if a.jsonOut() {
				return a.printJSON(map[string]any{"host": host, "user": user.Username, "storage": where})
			}
			_, err = fmt.Fprintf(a.stdout, "Logged in to %s as %s (token stored in %s)\n", host, user.Username, where)
			return err
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "Cloudzilla URL, e.g. https://git.example.com")
	cmd.Flags().BoolVar(&withToken, "with-token", false, "read a personal access token from stdin instead of logging in through the browser")
	cmd.Flags().StringArrayVar(&scopes, "scope", nil, "scope to request (repeatable or space-separated; default repo:write)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL and code without opening a browser")
	cmd.Flags().BoolVar(&insecure, "insecure-storage", false, "store the token in a 0600 file instead of the OS keychain")
	return cmd
}

func (a *app) stdinToken() (string, error) {
	b, err := io.ReadAll(io.LimitReader(a.stdin, 4096))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", errors.New("empty token")
	}
	return token, nil
}

var errLoginCancelled = errors.New("login cancelled")

func (a *app) deviceLogin(ctx context.Context, host string, scopes []string, noBrowser bool) (string, error) {
	scopes, err := cli.ValidateDeviceScopes(scopes)
	if err != nil {
		return "", err
	}
	var deviceName string
	if a.hostname != nil {
		deviceName, _ = a.hostname()
	}
	var clock cli.Clock = cli.SystemClock{}
	if a.clock != nil {
		clock = a.clock
	}
	c := a.client(cli.Credentials{Host: host})
	dc, err := c.RequestDeviceCode(ctx, scopes, deviceName)
	if err != nil {
		return "", cancelled(ctx, err)
	}

	copied := a.copyClipboard != nil && a.copyClipboard(dc.UserCode) == nil
	// The URI comes off the wire, so only plain http(s) is handed to the OS opener.
	canOpen := !noBrowser && a.stdinTTY && a.stdoutTTY && a.openBrowser != nil && isWebURL(dc.VerificationURI)
	opened := canOpen && a.openBrowser(dc.VerificationURI) == nil

	_, _ = fmt.Fprintf(a.stderr, "Your one-time code: %s", dc.UserCode)
	if copied {
		_, _ = fmt.Fprint(a.stderr, " (copied to the clipboard)")
	}
	_, _ = fmt.Fprintln(a.stderr)
	if opened {
		_, _ = fmt.Fprintf(a.stderr, "Opening %s in your browser; enter the code there.\n", dc.VerificationURI)
	} else {
		_, _ = fmt.Fprintf(a.stderr, "Open %s in a browser, sign in and enter the code.\n", dc.VerificationURI)
	}
	_, _ = fmt.Fprintln(a.stderr, "Waiting for approval (Ctrl-C to cancel)...")

	token, err := c.PollDeviceToken(ctx, dc, clock)
	if err != nil {
		return "", cancelled(ctx, err)
	}
	return token, nil
}

func cancelled(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errLoginCancelled
	}
	return err
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func statusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the host, user and token storage; fails when logged out",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.newStore(false)
			if err != nil {
				return err
			}
			creds, where, err := cli.Resolve(a.getenv, store)
			if err != nil {
				return err
			}
			user, err := a.client(creds).CurrentUser(cmd.Context())
			if err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(map[string]any{"host": creds.Host, "user": user.Username, "storage": where})
			}
			_, err = fmt.Fprintf(a.stdout, "Host:    %s\nUser:    %s\nStorage: %s\n", creds.Host, user.Username, where)
			return err
		},
	}
}

func logoutCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.newStore(false)
			if err != nil {
				return err
			}
			if err := store.Delete(); err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printJSON(map[string]any{"logged_out": true})
			}
			_, err = fmt.Fprintln(a.stdout, "Logged out")
			return err
		},
	}
}
