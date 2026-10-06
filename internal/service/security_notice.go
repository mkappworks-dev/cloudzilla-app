package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// notifySecurityChange mails userID about a change to how their account signs
// in, in the background: the change already happened, so a failure is only logged.
func notifySecurityChange(email *EmailService, users *store.UserStore, userID int64, kind string, compose func(username string) (subject, body string)) {
	if email == nil {
		return
	}
	concurrency.Go(kind+".notice", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := sendSecurityNotice(ctx, email, users, userID, compose); err != nil {
			slog.Error(kind+" notice", "user_id", userID, "error", err)
		}
	})
}

// sendSecurityNotice is notifySecurityChange in the foreground, for callers
// such as the CLI that exit before a background send would finish.
func sendSecurityNotice(ctx context.Context, email *EmailService, users *store.UserStore, userID int64, compose func(username string) (subject, body string)) error {
	if email == nil {
		return nil
	}
	u, err := users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	subject, body := compose(u.Username)
	if err := email.SendSecurityNotice(u, subject, body); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

func passwordChangedNotice(username string) (subject, body string) {
	return "Your Cloudzilla password was changed",
		fmt.Sprintf("<p>The password of the Cloudzilla account <strong>@%s</strong> was changed, and every session signed in to it was signed out.</p>"+
			"<p>If you didn't change it, someone else can sign in as you. Tell your administrator.</p>", html.EscapeString(username))
}

func totpChangedNotice(enabled bool) func(username string) (subject, body string) {
	return func(username string) (string, string) {
		user := html.EscapeString(username)
		if enabled {
			return "Two-factor authentication was turned on for your Cloudzilla account",
				fmt.Sprintf("<p>Signing in to <strong>@%s</strong> now needs a code from an authenticator app.</p>"+
					"<p>If you didn't turn it on, someone else was signed in as you, and you may not be able to sign in. Tell your administrator.</p>", user)
		}
		return "Two-factor authentication was turned off for your Cloudzilla account",
			fmt.Sprintf("<p>Signing in to <strong>@%s</strong> no longer needs a code from an authenticator app.</p>"+
				"<p>If you didn't turn it off, someone else can sign in as you. Turn it back on under Account settings → Security and tell your administrator.</p>", user)
	}
}
