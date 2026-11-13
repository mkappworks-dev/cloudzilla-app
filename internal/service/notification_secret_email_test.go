package service

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestFormatNotifEmail_SecretTypesHaveTextAndNoHint(t *testing.T) {
	for _, typ := range []model.NotificationType{model.NotifSecretAlert, model.NotifSecretAllowed, model.NotifTokenRevoked} {
		n := &model.Notification{Type: typ, ActorName: "bob", OwnerName: "o", RepoName: "r", SubjectURL: "/o/r/security/secrets/1", SubjectTitle: "aws_access_key"}
		subject, body := formatNotifEmail(n)
		if strings.Contains(subject, "New notification") || strings.Contains(body, "You have a new notification") {
			t.Errorf("%s fell back to the generic text", typ)
		}
		if strings.Contains(subject+body, "AKIA") {
			t.Errorf("%s leaked a hint", typ)
		}
	}
}
