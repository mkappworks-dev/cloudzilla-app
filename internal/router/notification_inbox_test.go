package router_test

// Integration tests for the inbox's Unsubscribe action through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestInboxUnsubscribe_MutesThreadAndRerendersCallersView(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	username := "testuser_" + suffix
	userID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "actor_"+suffix)
	repoID := testutil.SeedRepo(t, db, userID, username, suffix)
	session := superadminJWT(t, userID, username)

	ns := store.NewNotificationStore(db)
	notif := func(number int64, title string) int64 {
		n := &model.Notification{
			UserID: userID, ActorID: actorID, ActorName: "actor", Type: model.NotifIssueComment, RepoID: repoID,
			RepoName: "testrepo_" + suffix, OwnerName: username, SubjectID: number, SubjectKind: model.ThreadKindIssue,
			SubjectURL: "/x", SubjectTitle: title,
		}
		if err := ns.Create(t.Context(), n); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return n.ID
	}
	muted := notif(1, "Muted thread title")
	notif(2, "Kept thread title")

	post := func(htmx bool) (int, string) {
		req := browserRequest(http.MethodPost, "/api/notifications/unsubscribe?filter=unread", session, url.Values{"ids": {strconv.FormatInt(muted, 10)}})
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rr := serve(h, req)
		return rr.Code, rr.Body.String()
	}

	code, body := post(true)
	if code != http.StatusOK {
		t.Fatalf("htmx: want 200, got %d: %s", code, body)
	}
	if strings.Contains(body, "Muted thread title") || !strings.Contains(body, "Kept thread title") {
		t.Errorf("re-rendered unread view should drop the muted thread and keep the other: %s", body)
	}
	st, err := svc.ThreadSubscription.Status(t.Context(), userID, repoID, model.ThreadKindIssue, 1)
	if err != nil || st.State != model.ThreadStateMuted {
		t.Errorf("thread state = %+v, %v; want muted", st, err)
	}

	if code, _ := post(false); code != http.StatusNoContent {
		t.Errorf("plain post: want 204, got %d", code)
	}
}
