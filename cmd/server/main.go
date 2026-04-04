package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/db"
	"github.com/mkappworks/cloudzilla/internal/model"
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

	go runEmailDigest(context.Background(), services)

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

func runEmailDigest(ctx context.Context, svc *service.Services) {
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 8, 0, 0, 0, time.UTC)
		if now.Hour() < 8 {
			next = time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, time.UTC)
		}
		timer := time.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		today := time.Now().UTC().Weekday()
		modes := []string{"daily"}
		if today == time.Monday {
			modes = append(modes, "weekly")
		}
		for _, mode := range modes {
			users, err := svc.User.ListUsersForDigest(ctx, mode)
			if err != nil {
				continue
			}
			for _, u := range users {
				notifs, err := svc.Notification.ListUnreadByUser(ctx, u.ID)
				if err != nil || len(notifs) == 0 {
					continue
				}
				body := buildDigestBody(u, notifs)
				subject := fmt.Sprintf("Your %s Cloudzilla digest", mode)
				_ = svc.Email.Send(u.Email, subject, body)
			}
		}
	}
}

func buildDigestBody(u model.User, notifs []model.Notification) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<h2>Hello %s,</h2><p>Here are your unread notifications:</p><ul>", html.EscapeString(u.Username)))
	for _, n := range notifs {
		sb.WriteString(fmt.Sprintf("<li><a href=\"%s\">%s/%s #%d</a> — %s by %s</li>",
			n.SubjectURL, html.EscapeString(n.OwnerName), html.EscapeString(n.RepoName), n.SubjectID, string(n.Type), html.EscapeString(n.ActorName)))
	}
	sb.WriteString("</ul>")
	return sb.String()
}
