package service

import (
	"context"
	"fmt"
	"html"
	"net/smtp"

	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
)

// EmailService sends transactional emails via SMTP. Email is skipped when SMTP host is empty.
type EmailService struct {
	cfg config.SMTPConfig
}

// NewEmailService creates an EmailService from the given SMTP configuration.
func NewEmailService(cfg config.SMTPConfig) *EmailService {
	return &EmailService{cfg: cfg}
}

func (s *EmailService) Send(to, subject, htmlBody string) error {
	if s.cfg.Host == "" {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, to, subject, htmlBody)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	return smtp.SendMail(addr, auth, s.cfg.From, []string{to}, []byte(msg))
}

func (s *EmailService) SendNotification(ctx context.Context, user *model.User, notif *model.Notification) error {
	if !user.EmailNotifications || user.Email == "" {
		return nil
	}
	subject, body := formatNotifEmail(notif)
	return s.Send(user.Email, subject, body)
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
