package main

import (
	"errors"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
	"github.com/spf13/cobra"
)

// addRepoFlag registers -R/--repo; pair it with app.targetRepo.
func addRepoFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVarP(target, "repo", "R", "", "repository as owner/repo (default: the origin remote of the current checkout)")
}

// targetRepo picks the repository from a positional argument, then -R, then the origin remote of the working directory.
func (a *app) targetRepo(host, flag string, args []string) (cli.Repo, error) {
	switch {
	case flag != "" && len(args) > 0:
		return cli.Repo{}, errors.New("give the repository either as an argument or with -R, not both")
	case flag != "":
		return cli.ParseRepo(flag, host)
	case len(args) > 0:
		return cli.ParseRepo(args[0], host)
	}
	getwd := a.getwd
	if getwd == nil {
		getwd = os.Getwd
	}
	dir, err := getwd()
	if err != nil {
		return cli.Repo{}, err
	}
	return cli.RepoFromRemote(dir, host)
}

func (a *app) login() (cli.Credentials, *cli.Client, error) {
	store, err := a.newStore(false)
	if err != nil {
		return cli.Credentials{}, nil, err
	}
	creds, _, err := cli.Resolve(a.getenv, store)
	if err != nil {
		return cli.Credentials{}, nil, err
	}
	return creds, a.client(creds), nil
}
