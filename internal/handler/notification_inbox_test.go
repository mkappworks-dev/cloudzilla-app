package handler_test

// Integration tests for the inbox endpoints: bulk done/unsubscribe, single and all mark-read,
// and the unread count. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func inboxRouter(h *handler.Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/notifications/read-all", h.MarkAllNotificationsRead)
	r.Post("/api/notifications/done", h.MarkNotificationsDone)
	r.Post("/api/notifications/unsubscribe", h.UnsubscribeNotifications)
	r.Patch("/api/notifications/{id}", h.MarkNotificationRead)
	r.Get("/api/notifications/unread-count", h.GetUnreadCount)
	return middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r)
}

// inboxWorld is a recipient with a repo and an actor whose events land in the recipient's inbox.
type inboxWorld struct {
	t         *testing.T
	db        *sql.DB
	h         *handler.Handler
	recipient signedInUser
	actorID   int64
	repo      seededRepo
	notifs    *store.NotificationStore
}

func newInboxWorld(t *testing.T) *inboxWorld {
	t.Helper()
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)
	return &inboxWorld{
		t: t, db: db, h: newPageHandler(t, db), recipient: repo.owner, repo: repo,
		actorID: testutil.SeedUser(t, db, "actor_"+testutil.UniqueSuffix(t)),
		notifs:  store.NewNotificationStore(db),
	}
}

// notify adds an unread issue-comment notification about issue number in the recipient's repo.
func (w *inboxWorld) notify(number int64) int64 {
	w.t.Helper()
	n := &model.Notification{
		UserID: w.recipient.id, ActorID: w.actorID, ActorName: "somebody", Type: model.NotifIssueComment,
		RepoID: w.repo.id, RepoName: w.repo.name, OwnerName: w.repo.owner.name,
		SubjectID: number, SubjectKind: model.ThreadKindIssue,
	}
	if err := w.notifs.Create(w.t.Context(), n); err != nil {
		w.t.Fatalf("Create notification: %v", err)
	}
	return n.ID
}

func (w *inboxWorld) do(method, target string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.doAs(w.recipient.token, method, target, form, htmx)
}

func (w *inboxWorld) doAs(token, method, target string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	w.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	inboxRouter(w.h).ServeHTTP(rr, req)
	return rr
}

func (w *inboxWorld) unread() int {
	w.t.Helper()
	n, err := w.notifs.CountUnread(w.t.Context(), w.recipient.id)
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

func ids(ids ...int64) url.Values {
	v := url.Values{}
	for _, id := range ids {
		v.Add("ids", strconv.FormatInt(id, 10))
	}
	return v
}

// unreadBadge matches the inbox header's "N unread" counter.
func unreadBadge(n int) *regexp.Regexp {
	return regexp.MustCompile(`>` + strconv.Itoa(n) + `</span> unread`)
}

func TestMarkNotificationsDone_MarksTheSelectedOnesRead(t *testing.T) {
	w := newInboxWorld(t)
	first, second, third := w.notify(1), w.notify(2), w.notify(3)

	rr := w.do(http.MethodPost, "/api/notifications/done", ids(first, second), false)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s; want 204", rr.Code, rr.Body)
	}
	if got := w.unread(); got != 1 {
		t.Errorf("unread = %d; want only notification %d left", got, third)
	}
}

func TestMarkNotificationsDone_IgnoresJunkIDsAndOtherUsersNotifications(t *testing.T) {
	w := newInboxWorld(t)
	mine := w.notify(1)
	other := newInboxWorld(t)
	theirs := other.notify(1)

	form := ids(mine, theirs)
	form.Add("ids", "abc")
	form.Add("ids", "-4")
	form.Add("ids", "0")
	if rr := w.do(http.MethodPost, "/api/notifications/done", form, false); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s; want 204", rr.Code, rr.Body)
	}
	if got := w.unread(); got != 0 {
		t.Errorf("own unread = %d; want 0", got)
	}
	if got := other.unread(); got != 1 {
		t.Errorf("the other user's unread = %d; want it untouched at 1", got)
	}
}

func TestMarkNotificationsDone_ActsOnAtMostOneHundredIDs(t *testing.T) {
	w := newInboxWorld(t)
	all := make([]int64, 101)
	for i := range all {
		all[i] = w.notify(int64(i + 1))
	}

	if rr := w.do(http.MethodPost, "/api/notifications/done", ids(all...), false); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s; want 204", rr.Code, rr.Body)
	}
	if got := w.unread(); got != 1 {
		t.Errorf("unread = %d; want the 101st id left alone", got)
	}
}

func TestMarkNotificationsDone_HTMXRerendersTheCallersFilter(t *testing.T) {
	w := newInboxWorld(t)
	first, _ := w.notify(1), w.notify(2)

	rr := w.do(http.MethodPost, "/api/notifications/done?filter=unread", ids(first), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s; want 200", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	assertContains(t, body, `id="notifications-view"`)
	assertContains(t, body, `/api/notifications/read-all?filter=unread`)
	if !unreadBadge(1).MatchString(body) {
		t.Errorf("want one unread notification left in:\n%s", body)
	}
	if strings.Contains(body, "<html") {
		t.Error("want only the swap fragment, not a full page")
	}
}

func TestMarkNotificationsDone_NeedsASignIn(t *testing.T) {
	w := newInboxWorld(t)
	id := w.notify(1)

	for _, path := range []string{"/api/notifications/done", "/api/notifications/unsubscribe", "/api/notifications/read-all"} {
		if rr := w.doAs("", http.MethodPost, path, ids(id), false); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d; want 401", path, rr.Code)
		}
	}
	if got := w.unread(); got != 1 {
		t.Errorf("unread = %d; want 1", got)
	}
}

func TestMarkNotificationsDone_ReportsStorageFailureAs500(t *testing.T) {
	db := openSchemalessDB(t)
	h := newPageHandler(t, db)
	token := makeIssueJWT(t, 1, "someone")

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/done", strings.NewReader(ids(5).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	inboxRouter(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "failed to mark done") {
		t.Errorf("response = %d %s; want 500 failed to mark done", rr.Code, rr.Body)
	}
}

func TestMarkNotificationsDone_HTMXReports500WhenTheInboxCannotBeLoaded(t *testing.T) {
	db := openSchemalessDB(t)
	h := newPageHandler(t, db)
	token := makeIssueJWT(t, 1, "someone")

	// No ids: nothing is written, so only the re-render touches the database.
	req := httptest.NewRequest(http.MethodPost, "/api/notifications/done", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	inboxRouter(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "failed to load notifications") {
		t.Errorf("response = %d %s; want 500 failed to load notifications", rr.Code, rr.Body)
	}
}

func TestUnsubscribeNotifications_MutesTheThreadAndClearsItsUnread(t *testing.T) {
	w := newInboxWorld(t)
	first, sameThread, otherThread := w.notify(7), w.notify(7), w.notify(8)

	rr := w.do(http.MethodPost, "/api/notifications/unsubscribe", ids(first), false)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s; want 204", rr.Code, rr.Body)
	}
	if got := w.unread(); got != 1 {
		t.Errorf("unread = %d; want only the other thread's notification %d left (%d was on the muted thread)", got, otherThread, sameThread)
	}
	var state string
	err := w.db.QueryRow(
		`SELECT state FROM thread_subscriptions WHERE user_id = $1 AND repo_id = $2 AND kind = 'issue' AND number = 7`,
		w.recipient.id, w.repo.id).Scan(&state)
	if err != nil || state != model.ThreadStateMuted {
		t.Errorf("thread state = %q, %v; want muted", state, err)
	}
}

func TestUnsubscribeNotifications_HTMXRerenders(t *testing.T) {
	w := newInboxWorld(t)
	first, _ := w.notify(1), w.notify(2)

	rr := w.do(http.MethodPost, "/api/notifications/unsubscribe", ids(first), true)
	if rr.Code != http.StatusOK || !unreadBadge(1).MatchString(rr.Body.String()) {
		t.Errorf("response = %d %s; want the inbox with one unread", rr.Code, rr.Body)
	}
}

func TestUnsubscribeNotifications_ReportsMuteFailureAs500(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)
	// newNotifHandler's service has no thread subscriptions wired, so muting fails.
	h := newNotifHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", strings.NewReader(ids(1).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+repo.owner.token)
	rr := httptest.NewRecorder()
	inboxRouter(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "failed to unsubscribe") {
		t.Errorf("response = %d %s; want 500 failed to unsubscribe", rr.Code, rr.Body)
	}
}

func TestMarkNotificationRead(t *testing.T) {
	t.Run("marks one read", func(t *testing.T) {
		w := newInboxWorld(t)
		first, _ := w.notify(1), w.notify(2)

		rr := w.do(http.MethodPatch, fmt.Sprintf("/api/notifications/%d", first), nil, false)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d: %s; want 204", rr.Code, rr.Body)
		}
		if got := w.unread(); got != 1 {
			t.Errorf("unread = %d; want 1", got)
		}
	})

	t.Run("leaves another user's notification alone", func(t *testing.T) {
		w := newInboxWorld(t)
		other := newInboxWorld(t)
		theirs := other.notify(1)

		w.do(http.MethodPatch, fmt.Sprintf("/api/notifications/%d", theirs), nil, false)
		if got := other.unread(); got != 1 {
			t.Errorf("other user's unread = %d; want 1", got)
		}
	})

	t.Run("rejects a non-numeric id", func(t *testing.T) {
		w := newInboxWorld(t)
		if rr := w.do(http.MethodPatch, "/api/notifications/abc", nil, false); rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d; want 400", rr.Code)
		}
	})

	t.Run("needs a sign-in", func(t *testing.T) {
		w := newInboxWorld(t)
		id := w.notify(1)
		if rr := w.doAs("", http.MethodPatch, fmt.Sprintf("/api/notifications/%d", id), nil, false); rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d; want 401", rr.Code)
		}
	})

	t.Run("htmx re-renders the inbox", func(t *testing.T) {
		w := newInboxWorld(t)
		first, _ := w.notify(1), w.notify(2)

		rr := w.do(http.MethodPatch, fmt.Sprintf("/api/notifications/%d", first), nil, true)
		if rr.Code != http.StatusOK || !unreadBadge(1).MatchString(rr.Body.String()) {
			t.Errorf("response = %d %s; want the inbox with one unread", rr.Code, rr.Body)
		}
	})

	t.Run("reports storage failure as 500", func(t *testing.T) {
		h := newPageHandler(t, openSchemalessDB(t))
		req := httptest.NewRequest(http.MethodPatch, "/api/notifications/3", nil)
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, 1, "someone"))
		rr := httptest.NewRecorder()
		inboxRouter(h).ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d; want 500", rr.Code)
		}
	})
}

func TestMarkAllNotificationsRead(t *testing.T) {
	t.Run("htmx re-renders the inbox with nothing unread", func(t *testing.T) {
		w := newInboxWorld(t)
		w.notify(1)
		w.notify(2)

		rr := w.do(http.MethodPost, "/api/notifications/read-all", nil, true)
		if rr.Code != http.StatusOK || !unreadBadge(0).MatchString(rr.Body.String()) {
			t.Errorf("response = %d %s; want the inbox with 0 unread", rr.Code, rr.Body)
		}
		if got := w.unread(); got != 0 {
			t.Errorf("unread = %d; want 0", got)
		}
	})

	t.Run("reports storage failure as 500", func(t *testing.T) {
		h := newPageHandler(t, openSchemalessDB(t))
		req := httptest.NewRequest(http.MethodPost, "/api/notifications/read-all", nil)
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, 1, "someone"))
		rr := httptest.NewRecorder()
		inboxRouter(h).ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d; want 500", rr.Code)
		}
	})
}

func TestGetUnreadCount_CountsTheCallersUnreadNotifications(t *testing.T) {
	w := newInboxWorld(t)
	first := w.notify(1)
	w.notify(2)
	w.notify(3)
	w.do(http.MethodPatch, fmt.Sprintf("/api/notifications/%d", first), nil, false)

	rr := w.do(http.MethodGet, "/api/notifications/unread-count", nil, false)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"count":2}` {
		t.Errorf("response = %d %s; want {\"count\":2}", rr.Code, rr.Body)
	}
}
