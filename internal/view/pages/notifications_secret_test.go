package pages_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestNotifications_SecretTypesHaveTheirOwnTitles(t *testing.T) {
	mk := func(id int64, typ model.NotificationType, actor, title string) model.NotificationGroup {
		return model.NotificationGroup{Notification: model.Notification{
			ID: id, ActorName: actor, Type: typ, OwnerName: "alice", RepoName: "demo",
			SubjectID: id, SubjectURL: "/alice/demo/security/secrets", SubjectTitle: title, CreatedAt: time.Now(),
		}, Count: 1, Unread: 1}
	}
	data := view.NotificationsData{
		Groups: []model.NotificationGroup{
			mk(1, model.NotifSecretAlert, "Cloudzilla", "7 secrets"),
			mk(2, model.NotifSecretAllowed, "bob", "aws_access_key"),
			mk(3, model.NotifTokenRevoked, "Cloudzilla", "ci"),
		},
		Filter: "inbox", Page: 1, TotalPages: 1, PerPage: 25, Total: 3, InboxCount: 3, UnreadCount: 3,
	}
	var sb strings.Builder
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"Secret scanning found 7 secrets", "@bob allowed a secret to be pushed", "Your token ci was revoked"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
}
