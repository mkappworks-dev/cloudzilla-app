package handler_test

// Integration tests for the thread subscription endpoint. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type threadSubEnv struct {
	db       *sql.DB
	router   http.Handler
	svc      *service.Services
	ownerID  int64
	owner    string
	repo     string
	repoID   int64
	viewerID int64
	viewer   string
	numbers  map[string]int
}

func newThreadSubEnv(t *testing.T) *threadSubEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	e := &threadSubEnv{db: db, numbers: map[string]int{}}
	e.ownerID = testutil.SeedUser(t, db, suffix)
	e.owner = "testuser_" + suffix
	e.repoID = testutil.SeedRepo(t, db, e.ownerID, e.owner, suffix)
	e.repo = "testrepo_" + suffix
	e.viewerID = testutil.SeedUser(t, db, suffix+"v")
	e.viewer = "testuser_" + suffix + "v"

	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName}}
	stores := &store.Stores{
		User:               store.NewUserStore(db),
		Repo:               store.NewRepoStore(db),
		Issue:              store.NewIssueStore(db),
		Pull:               store.NewPullStore(db),
		Discussion:         store.NewDiscussionStore(db),
		Watch:              store.NewWatchStore(db),
		Org:                store.NewOrgStore(db),
		ThreadSubscription: store.NewThreadSubscriptionStore(db),
		SiteSetting:        store.NewSiteSettingStore(db),
		AuditLog:           store.NewAuditLogStore(db),
	}
	repoSvc := service.NewRepoService(stores.Repo, stores.User, stores.Org, nil, nil, config.GitConfig{})
	e.svc = &service.Services{
		User:               service.NewUserService(stores.User, cfg.Auth),
		Repo:               repoSvc,
		Issue:              service.NewIssueService(stores.Issue, stores.Repo, stores.Pull, repoSvc),
		Pull:               service.NewPullService(stores.Pull, stores.Repo, repoSvc),
		Discussion:         service.NewDiscussionService(stores.Discussion, stores.Repo),
		Watch:              service.NewWatchService(stores.Watch, stores.Repo),
		ThreadSubscription: service.NewThreadSubscriptionService(stores.ThreadSubscription, stores.Watch),
		SiteSetting:        service.NewSiteSettingService(stores.SiteSetting, stores.User),
		AuditLog:           service.NewAuditService(stores.AuditLog),
	}
	h := handler.New(e.svc, cfg)

	r := chi.NewRouter()
	r.Put("/api/repos/{owner}/{repo}/issues/{number}/subscription", h.SetIssueSubscription)
	r.Put("/api/repos/{owner}/{repo}/pulls/{number}/subscription", h.SetPullSubscription)
	r.Put("/api/repos/{owner}/{repo}/discussions/{number}/subscription", h.SetDiscussionSubscription)
	e.router = middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(r)

	ctx := t.Context()
	issue := &model.Issue{RepoID: e.repoID, AuthorID: e.ownerID, Title: "i", State: model.IssueStateOpen}
	if err := stores.Issue.Create(ctx, issue); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	e.numbers[model.ThreadKindIssue] = issue.Number
	pull := &model.PullRequest{RepoID: e.repoID, AuthorID: e.ownerID, Title: "p", State: model.PRStateOpen, HeadBranch: "f", BaseBranch: "main"}
	if err := stores.Pull.Create(ctx, pull); err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	e.numbers[model.ThreadKindPull] = pull.Number
	var categoryID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM discussion_categories ORDER BY id LIMIT 1`).Scan(&categoryID); err != nil {
		t.Skipf("no discussion category seeded: %v", err)
	}
	disc := &model.Discussion{RepoID: e.repoID, CategoryID: categoryID, Title: "d", AuthorID: e.ownerID, AuthorName: e.owner}
	if err := stores.Discussion.Create(ctx, disc); err != nil {
		t.Fatalf("seed discussion: %v", err)
	}
	e.numbers[model.ThreadKindDiscussion] = disc.Number
	return e
}

var threadSegments = map[string]string{
	model.ThreadKindIssue:      "issues",
	model.ThreadKindPull:       "pulls",
	model.ThreadKindDiscussion: "discussions",
}

func (e *threadSubEnv) put(t *testing.T, kind string, number int, state string, token string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/repos/" + e.owner + "/" + e.repo + "/" + threadSegments[kind] + "/" + strconv.Itoa(number) + "/subscription"
	req := httptest.NewRequest(http.MethodPut, url, strings.NewReader(`{"state":"`+state+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e *threadSubEnv) status(t *testing.T, kind string) service.ThreadStatus {
	t.Helper()
	st, err := e.svc.ThreadSubscription.Status(t.Context(), e.viewerID, e.repoID, kind, int64(e.numbers[kind]))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func TestSetThreadSubscription_AnonymousIs401(t *testing.T) {
	e := newThreadSubEnv(t)
	for kind := range threadSegments {
		if rr := e.put(t, kind, e.numbers[kind], "subscribed", "", false); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", kind, rr.Code)
		}
	}
}

func TestSetThreadSubscription_SubscribeWritesManualAndMuteWritesMuted(t *testing.T) {
	e := newThreadSubEnv(t)
	token := makeIssueJWT(t, e.viewerID, e.viewer)
	for kind := range threadSegments {
		t.Run(kind, func(t *testing.T) {
			n := e.numbers[kind]
			if rr := e.put(t, kind, n, "subscribed", token, false); rr.Code != http.StatusOK {
				t.Fatalf("subscribe: want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if st := e.status(t, kind); st.State != model.ThreadStateSubscribed || st.Reason != model.ThreadReasonManual {
				t.Errorf("after subscribe = %+v, want subscribed/manual", st)
			}
			if rr := e.put(t, kind, n, "muted", token, false); rr.Code != http.StatusOK {
				t.Fatalf("mute: want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if st := e.status(t, kind); st.State != model.ThreadStateMuted {
				t.Errorf("after mute = %+v, want muted", st)
			}
		})
	}
}

func TestSetThreadSubscription_HTMXGetsRerenderedSection(t *testing.T) {
	e := newThreadSubEnv(t)
	token := makeIssueJWT(t, e.viewerID, e.viewer)
	rr := e.put(t, model.ThreadKindPull, e.numbers[model.ThreadKindPull], "subscribed", token, true)
	body := rr.Body.String()
	for _, want := range []string{`id="thread-subscription"`, "Unsubscribe", "You subscribed to this thread."} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment missing %q: %s", want, body)
		}
	}
}

func TestSetThreadSubscription_MuteLeavesRepoWatchAlone(t *testing.T) {
	e := newThreadSubEnv(t)
	if err := e.svc.Watch.Watch(t.Context(), e.owner, e.repo, e.viewerID, model.WatchLevelWatching); err != nil {
		t.Fatalf("watch: %v", err)
	}
	token := makeIssueJWT(t, e.viewerID, e.viewer)
	if rr := e.put(t, model.ThreadKindIssue, e.numbers[model.ThreadKindIssue], "muted", token, false); rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if st := e.status(t, model.ThreadKindIssue); st.State != model.ThreadStateMuted {
		t.Errorf("status = %+v, want muted", st)
	}
	if got := e.svc.Watch.GetLevel(t.Context(), e.viewerID, e.repoID); got != model.WatchLevelWatching {
		t.Errorf("watch level = %q, want unchanged %q", got, model.WatchLevelWatching)
	}
}

func TestSetThreadSubscription_BadStateIs422(t *testing.T) {
	e := newThreadSubEnv(t)
	token := makeIssueJWT(t, e.viewerID, e.viewer)
	for _, state := range []string{"", "watching", "ignored"} {
		if rr := e.put(t, model.ThreadKindIssue, e.numbers[model.ThreadKindIssue], state, token, false); rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("state %q: want 422, got %d", state, rr.Code)
		}
	}
}

func TestSetThreadSubscription_MissingThreadIs404(t *testing.T) {
	e := newThreadSubEnv(t)
	token := makeIssueJWT(t, e.viewerID, e.viewer)
	for kind := range threadSegments {
		if rr := e.put(t, kind, 9999, "subscribed", token, false); rr.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", kind, rr.Code)
		}
	}
}

func TestSetThreadSubscription_UnreadableRepoIs404(t *testing.T) {
	e := newThreadSubEnv(t)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)
	token := makeIssueJWT(t, e.viewerID, e.viewer)
	for kind := range threadSegments {
		if rr := e.put(t, kind, e.numbers[kind], "subscribed", token, false); rr.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", kind, rr.Code)
		}
	}
	if st := e.status(t, model.ThreadKindIssue); st.State != "" {
		t.Errorf("a rejected request wrote %+v", st)
	}
}
