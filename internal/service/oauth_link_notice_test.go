package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type sentMail struct{ to, subject, body string }

func awaitMail(t *testing.T, sent <-chan sentMail) sentMail {
	t.Helper()
	select {
	case m := <-sent:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no notice was sent")
		return sentMail{}
	}
}

func TestOAuthLink_NoticesReachTheAccountDespiteMutedNotifications(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "notice password")
	testutil.Exec(t, db, `UPDATE users SET email_notifications = FALSE WHERE id = $1`, userID)

	sent := make(chan sentMail, 2)
	mail := NewEmailService(config.SMTPConfig{})
	mail.send = func(to, subject, body string) error {
		sent <- sentMail{to, subject, body}
		return nil
	}
	users := store.NewUserStore(db)
	svc := NewOAuthLinkService(users, store.NewOAuthStateStore(db), NewTOTPService(users), mail)

	googleEmail := "<b>" + suffix + "</b>@example.com"
	if _, err := svc.Link(ctx, userID, OAuthIdentity{Provider: "google", ID: "g_notice_" + suffix, Email: googleEmail, EmailVerified: true}); err != nil {
		t.Fatalf("Link: %v", err)
	}
	connected := awaitMail(t, sent)
	if connected.to != email || !strings.Contains(connected.subject, "connected") {
		t.Errorf("connect notice went to %q with subject %q; want %q and a connected subject", connected.to, connected.subject, email)
	}
	if !strings.Contains(connected.body, "&lt;b&gt;"+suffix+"&lt;/b&gt;@example.com") || strings.Contains(connected.body, "<b>"+suffix) {
		t.Errorf("connect notice does not name the escaped Google address: %s", connected.body)
	}

	if err := svc.Unlink(ctx, userID, "google", "notice password", ""); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	removed := awaitMail(t, sent)
	if removed.to != email || !strings.Contains(removed.subject, "removed") {
		t.Errorf("disconnect notice went to %q with subject %q; want %q and a removed subject", removed.to, removed.subject, email)
	}
}
