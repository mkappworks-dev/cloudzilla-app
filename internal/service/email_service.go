package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// EmailService sends transactional emails via SMTP. Email is skipped when SMTP host is empty.
type EmailService struct {
	cfg     config.SMTPConfig
	timeout time.Duration
	// Tests swap send to observe what SendNotification lets through.
	send func(to, subject, htmlBody string) error
}

// NewEmailService creates an EmailService from the given SMTP configuration.
func NewEmailService(cfg config.SMTPConfig) *EmailService {
	s := &EmailService{cfg: cfg, timeout: 30 * time.Second}
	s.send = s.Send
	return s
}

// Send does what smtp.SendMail does, but under one deadline: anonymous
// visitors can trigger mail, and a stalled server would otherwise pin a
// goroutine forever.
func (s *EmailService) Send(to, subject, htmlBody string) error {
	if !s.Enabled() {
		return nil
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, to, subject, htmlBody)

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port)), s.timeout)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		_ = conn.Close()
		return err
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return err
		}
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s *EmailService) SendNotification(ctx context.Context, user *model.User, notif *model.Notification) error {
	if !wantsEmail(user, notif.Type, model.EmailDigestImmediate) {
		return nil
	}
	subject, body := formatNotifEmail(notif)
	return s.send(user.Email, subject, body)
}

// The immediate path and the digest job share this gate so the per-type toggles apply to both.
func wantsEmail(u *model.User, t model.NotificationType, digestMode string) bool {
	if !u.EmailNotifications || u.Email == "" || u.EmailDigest != digestMode {
		return false
	}
	switch t {
	case model.NotifMention:
		return u.NotifyMention
	case model.NotifPRReview:
		return u.NotifyPRReview
	}
	return true
}

func formatNotifEmail(n *model.Notification) (subject, body string) {
	actor := html.EscapeString(n.ActorName)
	owner := html.EscapeString(n.OwnerName)
	repo := html.EscapeString(n.RepoName)
	switch n.Type {
	case model.NotifIssueComment:
		subject = fmt.Sprintf("[%s/%s] New comment on issue #%d", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> commented on issue <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	case model.NotifPRComment:
		subject = fmt.Sprintf("[%s/%s] New comment on pull request #%d", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> commented on pull request <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	case model.NotifIssueClosed:
		subject = fmt.Sprintf("[%s/%s] Issue #%d closed", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> closed issue <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	case model.NotifIssueReopened:
		subject = fmt.Sprintf("[%s/%s] Issue #%d reopened", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> reopened issue <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	case model.NotifPRMerged:
		subject = fmt.Sprintf("[%s/%s] Pull request #%d merged", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> merged pull request <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	case model.NotifPRClosed:
		subject = fmt.Sprintf("[%s/%s] Pull request #%d closed", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> closed pull request <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	case model.NotifPRReview:
		subject = fmt.Sprintf("[%s/%s] New review on pull request #%d", n.OwnerName, n.RepoName, n.SubjectID)
		body = fmt.Sprintf("<p><strong>%s</strong> reviewed pull request <a href=\"%s\">#%d</a> in %s/%s.</p>",
			actor, n.SubjectURL, n.SubjectID, owner, repo)
	default:
		subject = fmt.Sprintf("[%s/%s] New notification", n.OwnerName, n.RepoName)
		body = fmt.Sprintf("<p>You have a new notification from <strong>%s</strong> in %s/%s: <a href=\"%s\">view</a>.</p>",
			actor, owner, repo, n.SubjectURL)
	}
	return subject, body
}

func (s *EmailService) Enabled() bool {
	return s.cfg.Host != ""
}

func (s *EmailService) SendSignupLink(to, link string) error {
	subject, body := signupLinkEmail(link)
	return s.Send(to, subject, body)
}

func (s *EmailService) SendAccountExists(to, loginURL string) error {
	subject, body := accountExistsEmail(loginURL)
	return s.Send(to, subject, body)
}

func signupLinkEmail(link string) (subject, body string) {
	return "Finish creating your Cloudzilla account", fmt.Sprintf(
		`<p>Someone asked to create a Cloudzilla account with this email address.</p>`+
			`<p><a href="%s">Choose a username and password</a> to finish. The link works once and expires in 24 hours.</p>`+
			`<p>If this wasn't you, ignore this email.</p>`,
		html.EscapeString(link))
}

func accountExistsEmail(loginURL string) (subject, body string) {
	return "You already have a Cloudzilla account", fmt.Sprintf(
		`<p>Someone asked to create a Cloudzilla account with this email address, but it already has one.</p>`+
			`<p><a href="%s">Sign in</a> instead.</p>`+
			`<p>If this wasn't you, ignore this email.</p>`,
		html.EscapeString(loginURL))
}
