package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// notifySystem sends n to each of recipients except skipID (0 skips no one). notifications.actor_id is NOT NULL, so a
// system event with no acting user uses each recipient as their own actor.
func (s *NotificationService) notifySystem(ctx context.Context, repo *model.Repository, n model.Notification, recipients []int64, skipID int64) {
	for _, uid := range recipients {
		if uid == skipID {
			continue
		}
		r := n
		r.UserID = uid
		if r.ActorID == 0 {
			r.ActorID = uid
			r.ActorName = "Cloudzilla"
		}
		if s.create(ctx, repo, &r) {
			s.sendEmailAsync(r)
		}
	}
}

func secretAlertsURL(repo model.Repository) string {
	return fmt.Sprintf("/%s/%s/security/secrets", repo.OwnerName, repo.Name)
}

// NotifySecretAlert tells repo's admins that count findings were recorded, as one notification so a backfill sends a
// digest rather than a flood. alert is the first of them and names the page when count is 1.
func (s *NotificationService) NotifySecretAlert(ctx context.Context, repo model.Repository, alert *model.SecretScanAlert, count int) {
	managers, err := s.repos.ListManagers(ctx, &repo)
	if err != nil {
		slog.Error("NotifySecretAlert: failed to list repo admins", "repo_id", repo.ID, "error", err)
		return
	}
	n := model.Notification{
		Type:       model.NotifSecretAlert,
		RepoID:     repo.ID,
		RepoName:   repo.Name,
		OwnerName:  repo.OwnerName,
		SubjectURL: secretAlertsURL(repo),
	}
	if count <= 1 {
		n.SubjectID = int64(alert.Number)
		n.SubjectURL = fmt.Sprintf("%s/%d", n.SubjectURL, alert.Number)
		n.SubjectTitle = alert.SecretType
	} else {
		n.SubjectID = int64(count)
		n.SubjectTitle = fmt.Sprintf("%d secrets", count)
	}
	s.notifySystem(ctx, &repo, n, managers, 0)
}

// NotifySecretAllowed tells repo's admins that actor allowed alert's value to be pushed. The actor is not told.
func (s *NotificationService) NotifySecretAllowed(ctx context.Context, repo model.Repository, alert *model.SecretScanAlert, actor model.User) {
	managers, err := s.repos.ListManagers(ctx, &repo)
	if err != nil {
		slog.Error("NotifySecretAllowed: failed to list repo admins", "repo_id", repo.ID, "error", err)
		return
	}
	n := model.Notification{
		ActorID:      actor.ID,
		ActorName:    actor.Username,
		Type:         model.NotifSecretAllowed,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    int64(alert.Number),
		SubjectURL:   fmt.Sprintf("%s/%d", secretAlertsURL(repo), alert.Number),
		SubjectTitle: alert.SecretType,
	}
	s.notifySystem(ctx, &repo, n, managers, actor.ID)
}

// NotifyTokenRevoked tells ownerID that the instance deleted their token after it appeared in repo.
func (s *NotificationService) NotifyTokenRevoked(ctx context.Context, ownerID int64, token model.AccessToken, repo model.Repository) {
	n := model.Notification{
		Type:         model.NotifTokenRevoked,
		RepoID:       repo.ID,
		RepoName:     repo.Name,
		OwnerName:    repo.OwnerName,
		SubjectID:    token.ID,
		SubjectURL:   "/settings/tokens",
		SubjectTitle: token.Name,
	}
	s.notifySystem(ctx, &repo, n, []int64{ownerID}, 0)
}
