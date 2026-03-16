package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/db"
	"github.com/mkappworks/cloudzilla/internal/router"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/ssh"
	"github.com/mkappworks/cloudzilla/internal/store"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	database, err := db.Connect(cfg.Database)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	stores := store.New(database)
	services := service.New(stores, cfg)

	r := router.New(services, cfg, frontendFS)

	// Start SSH server
	sshSrv := ssh.New(cfg.Git, services)
	go func() {
		sshAddr := fmt.Sprintf(":%d", cfg.Git.SSHPort)
		slog.Info("ssh server starting", "addr", sshAddr)
		if err := sshSrv.ListenAndServe(); err != nil {
			slog.Error("ssh server error", "error", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		slog.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("http graceful shutdown failed", "error", err)
	}

	// Shutdown SSH server
	sshCtx, sshCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sshCancel()
	if err := sshSrv.Shutdown(sshCtx); err != nil {
		slog.Error("ssh graceful shutdown failed", "error", err)
	}

	slog.Info("server stopped")
}
