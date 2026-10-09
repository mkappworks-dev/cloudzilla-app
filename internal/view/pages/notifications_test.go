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

func TestNotifications_RowActions(t *testing.T) {
	var sb strings.Builder
	data := view.NotificationsData{
		Notifications: []model.Notification{
			{ID: 1, ActorName: "bob", Type: model.NotifIssueComment, RepoID: 10, OwnerName: "alice", RepoName: "watched", SubjectID: 1, SubjectURL: "/alice/watched/issues/1", CreatedAt: time.Now()},
			{ID: 2, ActorName: "bob", Type: model.NotifIssueComment, RepoID: 11, OwnerName: "alice", RepoName: "plain", SubjectID: 2, SubjectURL: "/alice/plain/issues/2", Read: true, CreatedAt: time.Now()},
			{ID: 3, ActorName: "bob", Type: model.NotifRepoTransfer, RepoID: 10, OwnerName: "alice", RepoName: "watched", SubjectID: 3, SubjectURL: "/repos/transfers", CreatedAt: time.Now()},
		},
		Filter: "inbox", Page: 1, TotalPages: 1, PerPage: 25, Total: 3, UnreadCount: 2,
		WatchedRepos: map[int64]bool{10: true},
	}
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	if got := strings.Count(out, `name="ids"`); got != 3 {
		t.Errorf("row checkboxes = %d, want 3", got)
	}
	if got := strings.Count(out, "Unsubscribe from alice/watched"); got != 1 {
		t.Errorf("per-row unsubscribe buttons for the watched repo = %d, want 1 (none on the transfer row)", got)
	}
	if strings.Contains(out, "Unsubscribe from alice/plain") {
		t.Error("unwatched repo must not offer unsubscribe")
	}
	if got := strings.Count(out, `hx-patch="/api/notifications/`); got != 2 {
		t.Errorf("Done buttons = %d, want 2 (read rows have none)", got)
	}
	for _, want := range []string{`hx-post="/api/notifications/done"`, `hx-post="/api/notifications/unsubscribe"`, `hx-include="#notifications-form"`} {
		if !strings.Contains(out, want) {
			t.Errorf("bulk bar missing %q", want)
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
