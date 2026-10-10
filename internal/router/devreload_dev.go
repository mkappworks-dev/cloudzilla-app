//go:build dev

package router

import (
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mkappworks-dev/cloudzilla-app/internal/devreload"
)

// devFrontendDir is watched for asset edits, which then need no rebuild. It
// matches the root cmd/server/embed_dev.go serves from, so `make dev` has to
// run from the repo root.
const devFrontendDir = "cmd/server/frontend"

// withDevProbes registers the reload stream on probeMux, which answers exact
// paths ahead of every middleware — the same reason /healthz escapes the setup
// redirect, so neither the setup check nor CSRF needs a dev-only exception.
func withDevProbes(probes map[string]http.HandlerFunc) map[string]http.HandlerFunc {
	hub := devreload.New(devreload.InstanceID())

	go func() {
		if err := hub.Watch(nil, devFrontendDir); err != nil {
			slog.Warn("dev reload: asset watch stopped", "error", err)
		}
	}()

	// Closing the streams on the shutdown signal keeps them from holding up the
	// graceful shutdown in main.go, which would otherwise cost the reloader's
	// whole kill_delay on every restart. signal.Notify fans out, so main.go's
	// own handler still receives this.
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		hub.Close()
	}()

	probes["/_dev/reload"] = hub.Events
	probes["/_dev/reload.js"] = hub.Script
	slog.Info("dev reload enabled", "stream", "/_dev/reload", "watching", devFrontendDir)
	return probes
}
