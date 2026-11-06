package pages_test

import (
	"context"
	"regexp"
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
		Groups: []model.NotificationGroup{{Notification: model.Notification{
			ID: 1, ActorName: "bob", Type: model.NotifPRMerged, OwnerName: "alice", RepoName: "demo",
			SubjectID: 7, SubjectURL: "/alice/demo/pulls/7", SubjectTitle: "Add retries", CreatedAt: time.Now(),
		}, Count: 1, Unread: 1}},
		Filter: "unread", Page: 3, TotalPages: 9, PerPage: 25, Total: 210, UnreadCount: 210,
	}
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		"Add retries",
		"@bob merged PR #7",
		`hx-post="/api/notifications/done?filter=unread&amp;page=3"`,
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
		Groups: []model.NotificationGroup{
			{Notification: model.Notification{ID: 1, ActorName: "bob", Type: model.NotifIssueComment, SubjectKind: model.ThreadKindIssue, RepoID: 10, OwnerName: "alice", RepoName: "demo", SubjectID: 1, SubjectURL: "/alice/demo/issues/1", CreatedAt: time.Now()}, Count: 1, Unread: 1},
			{Notification: model.Notification{ID: 2, ActorName: "bob", Type: model.NotifPRComment, SubjectKind: model.ThreadKindPull, RepoID: 11, OwnerName: "alice", RepoName: "plain", SubjectID: 2, SubjectURL: "/alice/plain/pulls/2", Read: true, CreatedAt: time.Now()}, Count: 1, Unread: 0},
			{Notification: model.Notification{ID: 3, ActorName: "bob", Type: model.NotifRepoTransfer, RepoID: 10, OwnerName: "alice", RepoName: "demo", SubjectID: 3, SubjectURL: "/repos/transfers", CreatedAt: time.Now()}, Count: 1, Unread: 1},
			{Notification: model.Notification{ID: 4, ActorName: "bob", Type: model.NotifMention, RepoID: 10, OwnerName: "alice", RepoName: "demo", SubjectID: 4, SubjectURL: "/alice/demo/wiki", CreatedAt: time.Now()}, Count: 1, Unread: 1},
		},
		Filter: "inbox", Page: 1, TotalPages: 1, PerPage: 25, Total: 4, UnreadCount: 3,
	}
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	if got := strings.Count(out, `name="ids"`); got != 4 {
		t.Errorf("row checkboxes = %d, want 4", got)
	}
	if got := strings.Count(out, `hx-post="/api/notifications/unsubscribe"`); got != 3 {
		t.Errorf("active unsubscribe hx-posts = %d, want 3 (2 row buttons + the bulk bar)", got)
	}
	if got := strings.Count(out, `title="Unsubscribe: mute this thread. Repo watch unchanged."`); got != 2 {
		t.Errorf("enabled Unsubscribe tooltips = %d, want 2", got)
	}
	for _, want := range []string{"A repository transfer has no thread to unsubscribe from", "This notification has no thread to unsubscribe from"} {
		if !strings.Contains(out, want) {
			t.Errorf("disabled Unsubscribe tooltip %q missing", want)
		}
	}
	if got := strings.Count(out, "disabled"); got != 2 {
		t.Errorf("disabled Unsubscribe buttons = %d, want 2 (transfer and null-kind rows)", got)
	}
	if strings.Contains(out, "stop watching") || strings.Contains(out, "not watching") {
		t.Error("Unsubscribe still talks about the repo watch")
	}
	if got := strings.Count(out, `data-notif-done>`); got != 3 {
		t.Errorf("Done buttons = %d, want 3 (read rows have none)", got)
	}
	for _, want := range []string{`hx-post="/api/notifications/done"`, `hx-post="/api/notifications/unsubscribe"`, `hx-include="#notifications-form"`, `title="Mute the selected threads"`} {
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
		data := view.NotificationsData{Groups: []model.NotificationGroup{{Notification: model.Notification{
			ID: 1, ActorName: "bob", Type: tc.typ, OwnerName: "alice", RepoName: "demo",
			SubjectID: 7, SubjectURL: "/alice/demo/pull/7", CreatedAt: time.Now(),
		}, Count: 1, Unread: 1}}}
		if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
			t.Fatalf("%s: render: %v", tc.typ, err)
		}
		if !strings.Contains(sb.String(), tc.want) {
			t.Errorf("%s: title %q not rendered", tc.typ, tc.want)
		}
	}
}

func TestNotifications_ReasonChipsKeepTheReadFilter(t *testing.T) {
	var sb strings.Builder
	data := view.NotificationsData{Filter: "unread", Reason: "assigned", Page: 1, TotalPages: 1, PerPage: 25}
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		`href="/notifications?filter=unread"`,
		`href="/notifications?filter=unread&amp;reason=mention"`,
		`href="/notifications?filter=unread&amp;reason=review_requested"`,
		`href="/notifications?filter=unread&amp;reason=subscribed"`,
		`href="/notifications?reason=assigned"`,
		`No unread notifications on threads you&#39;re assigned to.`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if !regexp.MustCompile(`href="/notifications\?filter=unread&amp;reason=assigned"[^>]*aria-current="page"`).MatchString(out) {
		t.Error("the active reason chip is not marked aria-current")
	}
}

func TestNotifications_ThreadCountAndShortcuts(t *testing.T) {
	var sb strings.Builder
	data := view.NotificationsData{
		Groups: []model.NotificationGroup{{Notification: model.Notification{
			ID: 9, ActorName: "bob", Type: model.NotifIssueComment, SubjectKind: model.ThreadKindIssue, OwnerName: "alice", RepoName: "demo",
			SubjectID: 4, SubjectURL: "/alice/demo/issues/4", CreatedAt: time.Now(),
		}, Count: 5, Unread: 2}},
		Filter: "inbox", Page: 1, TotalPages: 1, PerPage: 25, Total: 1,
	}
	if err := pages.Notifications(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		`>5 notifications on this thread<`,
		`id="notif-shortcuts"`,
		`aria-keyshortcuts="?"`,
		"Next notification",
		"Previous notification",
		"Mark as done",
		"Select or deselect",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}
