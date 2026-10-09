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

func TestNotifications_SubjectTitleAndPager(t *testing.T) {
	var sb strings.Builder
	data := view.NotificationsData{
		Notifications: []model.Notification{{
			ID: 1, ActorName: "bob", Type: model.NotifPRMerged, OwnerName: "alice", RepoName: "demo",
			SubjectID: 7, SubjectURL: "/alice/demo/pulls/7", SubjectTitle: "Add retries", CreatedAt: time.Now(),
		}},
		Filter: "unread", Page: 3, TotalPages: 9, PerPage: 25, Total: 210, UnreadCount: 210,
	}
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		"Add retries",
		"@bob merged PR #7",
		`/api/notifications/1?filter=unread&amp;page=3`,
		`href="/notifications?filter=unread&amp;page=2"`,
		`href="/notifications?filter=unread&amp;page=4"`,
		`href="/notifications?filter=unread&amp;page=9"`,
		"51–51",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

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
