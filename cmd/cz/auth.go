package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	var withToken, insecure bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Verify a personal access token and save it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host = cli.NormalizeHost(host)
			if host == "" {
				host = cli.NormalizeHost(a.getenv("CZ_HOST"))
			}
			if host == "" {
				return errors.New("no host; pass --host URL")
			}
			token, err := a.loginToken(withToken)
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
	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the token from stdin")
	cmd.Flags().BoolVar(&insecure, "insecure-storage", false, "store the token in a 0600 file instead of the OS keychain")
	return cmd
}

func (a *app) loginToken(withToken bool) (string, error) {
	var raw string
	switch {
	case withToken:
		b, err := io.ReadAll(io.LimitReader(a.stdin, 4096))
		if err != nil {
			return "", err
		}
		raw = string(b)
	case a.stdinTTY:
		fmt.Fprint(a.stderr, "Personal access token: ")
		s, err := a.readSecret()
		if err != nil {
			return "", fmt.Errorf("reading token: %w", err)
		}
		raw = s
	default:
		return "", errors.New("stdin is not a terminal; pass --with-token and pipe the token in")
	}
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", errors.New("empty token")
	}
	return token, nil
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
