package service

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestEmailService_Enabled(t *testing.T) {
	if NewEmailService(config.SMTPConfig{}).Enabled() {
		t.Error("no SMTP host must mean disabled")
	}
	if !NewEmailService(config.SMTPConfig{Host: "smtp.test"}).Enabled() {
		t.Error("an SMTP host must mean enabled")
	}
}

func TestEmailService_Send_StalledServer_TimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})
	s := NewEmailService(config.SMTPConfig{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, From: "cz@test.invalid"})
	s.timeout = 100 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- s.Send("user@test.invalid", "subject", "body") }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("want an error from a server that never answers")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send is still waiting on a server that never answers")
	}
}

func TestSignupLinkEmail_LinksAndEscapes(t *testing.T) {
	_, body := signupLinkEmail("https://cz.test/register/complete/abc")
	if !strings.Contains(body, `href="https://cz.test/register/complete/abc"`) {
		t.Errorf("body must link to the completion page:\n%s", body)
	}
	_, body = signupLinkEmail(`https://cz.test/"><script>x</script>`)
	if strings.Contains(body, "<script>") {
		t.Errorf("link must be HTML-escaped:\n%s", body)
	}
}

func TestAccountExistsEmail_LinksToSignIn(t *testing.T) {
	_, body := accountExistsEmail("https://cz.test/login")
	if !strings.Contains(body, `href="https://cz.test/login"`) {
		t.Errorf("body must link to sign-in:\n%s", body)
	}
}

func emailPrefsUser(edit func(u *model.User)) *model.User {
	u := &model.User{
		Email:              "alice@example.com",
		EmailNotifications: true,
		EmailDigest:        model.EmailDigestImmediate,
		NotifyMention:      true,
		NotifyPRReview:     true,
	}
	if edit != nil {
		edit(u)
	}
	return u
}

func TestWantsEmail(t *testing.T) {
	const (
		immediate = model.EmailDigestImmediate
		daily     = model.EmailDigestDaily
		weekly    = model.EmailDigestWeekly
	)
	tests := []struct {
		name       string
		edit       func(u *model.User)
		typ        model.NotificationType
		digestMode string
		want       bool
	}{
		{"all on, mention", nil, model.NotifMention, immediate, true},
		{"all on, pr review", nil, model.NotifPRReview, immediate, true},
		{"all on, untoggled type", nil, model.NotifIssueComment, immediate, true},

		{"master off, mention", func(u *model.User) { u.EmailNotifications = false }, model.NotifMention, immediate, false},
		{"master off, untoggled type", func(u *model.User) { u.EmailNotifications = false }, model.NotifIssueComment, immediate, false},
		{"no address", func(u *model.User) { u.Email = "" }, model.NotifIssueComment, immediate, false},

		{"daily user, immediate send", func(u *model.User) { u.EmailDigest = daily }, model.NotifIssueComment, immediate, false},
		{"daily user, daily digest", func(u *model.User) { u.EmailDigest = daily }, model.NotifIssueComment, daily, true},
		{"daily user, weekly digest", func(u *model.User) { u.EmailDigest = daily }, model.NotifIssueComment, weekly, false},
		{"weekly user, weekly digest", func(u *model.User) { u.EmailDigest = weekly }, model.NotifPRReview, weekly, true},
		{"never", func(u *model.User) { u.EmailDigest = model.EmailDigestNever }, model.NotifIssueComment, immediate, false},

		{"mention off", func(u *model.User) { u.NotifyMention = false }, model.NotifMention, immediate, false},
		{"mention off, pr review", func(u *model.User) { u.NotifyMention = false }, model.NotifPRReview, immediate, true},
		{"mention off, daily digest", func(u *model.User) { u.NotifyMention = false; u.EmailDigest = daily }, model.NotifMention, daily, false},
		{"pr review off", func(u *model.User) { u.NotifyPRReview = false }, model.NotifPRReview, immediate, false},
		{"pr review off, mention", func(u *model.User) { u.NotifyPRReview = false }, model.NotifMention, immediate, true},
		{"pr review off, weekly digest", func(u *model.User) { u.NotifyPRReview = false; u.EmailDigest = weekly }, model.NotifPRReview, weekly, false},
		{"both toggles off, untoggled type", func(u *model.User) { u.NotifyMention = false; u.NotifyPRReview = false }, model.NotifIssueComment, immediate, true},
		{"both toggles off, discussion reply", func(u *model.User) { u.NotifyMention = false; u.NotifyPRReview = false }, model.NotifDiscussionReply, immediate, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wantsEmail(emailPrefsUser(tt.edit), tt.typ, tt.digestMode); got != tt.want {
				t.Errorf("wantsEmail(%s, %s) = %v, want %v", tt.typ, tt.digestMode, got, tt.want)
			}
		})
	}
}

func TestEmailService_SendNotification_SendsOnlyWhatImmediatePrefsAllow(t *testing.T) {
	tests := []struct {
		name string
		edit func(u *model.User)
		typ  model.NotificationType
		want bool
	}{
		{"immediate, toggle on", nil, model.NotifMention, true},
		{"immediate, untoggled type", nil, model.NotifIssueComment, true},
		{"daily user", func(u *model.User) { u.EmailDigest = model.EmailDigestDaily }, model.NotifIssueComment, false},
		{"weekly user", func(u *model.User) { u.EmailDigest = model.EmailDigestWeekly }, model.NotifIssueComment, false},
		{"never user", func(u *model.User) { u.EmailDigest = model.EmailDigestNever }, model.NotifIssueComment, false},
		{"mention toggle off", func(u *model.User) { u.NotifyMention = false }, model.NotifMention, false},
		{"pr review toggle off", func(u *model.User) { u.NotifyPRReview = false }, model.NotifPRReview, false},
		{"master switch off", func(u *model.User) { u.EmailNotifications = false }, model.NotifIssueComment, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sentTo []string
			s := &EmailService{send: func(to, _, _ string) error {
				sentTo = append(sentTo, to)
				return nil
			}}
			u := emailPrefsUser(tt.edit)
			if err := s.SendNotification(context.Background(), u, &model.Notification{Type: tt.typ}); err != nil {
				t.Fatalf("SendNotification: %v", err)
			}
			if got := len(sentTo) == 1 && sentTo[0] == u.Email; got != tt.want || len(sentTo) > 1 {
				t.Errorf("sent to %v, want sent=%v", sentTo, tt.want)
			}
		})
	}
}
