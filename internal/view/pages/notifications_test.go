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

func TestNotifications_TitlesForDeliveredTypes(t *testing.T) {
	for _, tc := range []struct {
		typ  model.NotificationType
		want string
	}{
		{model.NotifPRReview, "@bob reviewed PR #7"},
		{model.NotifMention, "@bob mentioned you"},
		{model.NotifDiscussionReply, "@bob replied in a discussion"},
	} {
		var sb strings.Builder
		data := view.NotificationsData{Notifications: []model.Notification{{
			ID: 1, ActorName: "bob", Type: tc.typ, OwnerName: "alice", RepoName: "demo",
			SubjectID: 7, SubjectURL: "/alice/demo/pull/7", CreatedAt: time.Now(),
		}}}
		if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
			t.Fatalf("%s: render: %v", tc.typ, err)
		}
		if !strings.Contains(sb.String(), tc.want) {
			t.Errorf("%s: title %q not rendered", tc.typ, tc.want)
		}
	}
}
