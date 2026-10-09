package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Must stay a var: release builds set it with -ldflags "-X main.version=<tag>".
var version = "dev"

type app struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	stdinTTY   bool
	stdoutTTY  bool
	getenv     func(string) string
	newStore   func(insecure bool) (*cli.Store, error)
	readSecret func() (string, error)
	http       *http.Client
	runGit     func(ctx context.Context, env []string, args ...string) error
	getwd      func() (string, error)

	jsonFlag bool
}

func (a *app) jsonOut() bool { return a.jsonFlag || !a.stdoutTTY }

func (a *app) client(c cli.Credentials) *cli.Client {
	return &cli.Client{Host: c.Host, Token: c.Token, HTTP: a.http}
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "cz",
		Short:         "Cloudzilla command-line client",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&a.jsonFlag, "json", false, "print JSON (the default when stdout is not a terminal)")
	root.AddCommand(authCmd(a), apiCmd(a), repoCmd(a), issueCmd(a))
	return root
}

func main() {
	a := &app{
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		stdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		stdoutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		getenv:    os.Getenv,
		newStore: func(insecure bool) (*cli.Store, error) {
			return cli.NewStore(insecure)
		},
		readSecret: func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b), err
		},
	}
	if err := newRootCmd(a).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cz:", err)
		os.Exit(1)
	}
}
