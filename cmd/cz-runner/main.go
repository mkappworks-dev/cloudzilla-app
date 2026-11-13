// Command cz-runner claims workflow Jobs from a Cloudzilla server and runs each Step in a hardened Docker container.
// It only ever makes outbound HTTPS requests, so it works behind NAT.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runner"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
)

// Must stay a var: release builds set it with -ldflags "-X main.version=<tag>".
var version = "dev"

const defaultImage = "ubuntu:24.04"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cz-runner:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "cz-runner", Short: "Cloudzilla CI runner", Version: version, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "config file (default: <user config dir>/cz-runner/config.yaml)")
	root.AddCommand(registerCmd(), runCmd())
	return root
}

func configPath(cmd *cobra.Command) (string, error) {
	if p, _ := cmd.Flags().GetString("config"); p != "" {
		return p, nil
	}
	return runner.DefaultConfigPath()
}

func registerCmd() *cobra.Command {
	var server, token, name string
	var labels, images, allowed []string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Trade a one-time registration token for a runner token and write the config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := configPath(cmd)
			if err != nil {
				return err
			}
			if name == "" {
				name, _ = os.Hostname()
			}
			imageMap := map[string]string{}
			for _, l := range labels {
				imageMap[l] = defaultImage
			}
			for _, kv := range images {
				l, img, ok := strings.Cut(kv, "=")
				if !ok || l == "" || img == "" {
					return fmt.Errorf("--image %q must be label=image", kv)
				}
				imageMap[l] = img
			}
			resp, err := runner.Register(cmd.Context(), server, token, runnerapi.RegisterRequest{Name: name, Labels: labels, Version: version})
			if err != nil {
				return err
			}
			cfg := &runner.Config{Server: server, Token: resp.Token, Name: name, Labels: labels, Images: imageMap, AllowedImages: allowed}
			if err := cfg.Save(path); err != nil {
				return err
			}
			fmt.Printf("Registered runner %q (id %d); config written to %s\n", name, resp.RunnerID, path)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&server, "server", "", "Cloudzilla base URL, e.g. https://git.example.com")
	f.StringVar(&token, "token", "", "one-time registration token")
	f.StringSliceVar(&labels, "labels", nil, "labels this runner serves, matched against a Job's runs-on")
	f.StringVar(&name, "name", "", "runner name (default: hostname)")
	f.StringArrayVar(&images, "image", nil, "label=image: the image a Job with that label runs in (default "+defaultImage+")")
	f.StringSliceVar(&allowed, "allowed-image", nil, "image a workflow's container: may name; a trailing * matches a prefix (default: any)")
	for _, req := range []string{"server", "token", "labels"} {
		_ = cmd.MarkFlagRequired(req)
	}
	return cmd
}

func runCmd() *cobra.Command {
	lim := runner.DefaultLimits()
	var dockerBin string
	var once bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Claim and run Jobs until stopped",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := configPath(cmd)
			if err != nil {
				return err
			}
			cfg, err := runner.LoadConfig(path)
			if err != nil {
				return err
			}
			if err := lim.Validate(); err != nil {
				return err
			}
			client := &runner.Client{Server: cfg.Server, Token: cfg.Token, HTTP: &http.Client{Timeout: (runnerapi.MaxClaimWaitSeconds + 30) * time.Second}}
			p := &runner.Poller{Client: client, Labels: cfg.Labels, Once: once, Exec: &runner.Executor{
				Config: cfg, Limits: lim, API: client, Docker: dockerBin, Checkout: runner.GitCheckout,
			}}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			slog.Info("cz-runner started", "server", cfg.Server, "labels", cfg.Labels)
			return p.Loop(ctx)
		},
	}
	f := cmd.Flags()
	f.StringVar(&lim.CPUs, "cpus", lim.CPUs, "CPU limit per container")
	f.StringVar(&lim.Memory, "memory", lim.Memory, "memory limit per container (also the swap limit)")
	f.IntVar(&lim.PIDs, "pids-limit", lim.PIDs, "process limit per container")
	f.StringVar(&lim.User, "user", lim.User, "uid:gid containers run as; never root")
	f.StringVar(&dockerBin, "docker", "docker", "docker CLI to run")
	f.BoolVar(&once, "once", false, "exit after one Job")
	return cmd
}
