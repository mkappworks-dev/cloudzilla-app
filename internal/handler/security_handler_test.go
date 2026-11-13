package handler_test

// Integration tests for the Security tab: alert list, alert page and the close, reopen and allow actions. All tests
// require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const securityTestKey = "AKIAIOSFODNN7EXAMPLE"

type securityFixture struct {
	db      *sql.DB
	router  http.Handler
	repo    seededRepo
	svc     *service.Services
	headSHA string
}

func newSecurityFixture(t *testing.T, private bool) securityFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	repo := seedOwnedRepo(t, db, private)
	if _, err := gogit.PlainInit(filepath.Join(reposRoot, repo.owner.name, repo.name+".git"), true); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: reposRoot},
	}
	svc := service.New(store.New(db), cfg)
	content := "REGION=eu\nAWS_ACCESS_KEY_ID=" + securityTestKey + "\nBUCKET=b\n"
	if _, err := svc.Code.CommitFile(repo.owner.name, repo.name, "main", "config/deploy.env", []byte(content), raceAuthor, "Add deploy.env"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	head, _, err := svc.Code.ResolveRef(repo.owner.name, repo.name, "main")
	if err != nil {
		t.Fatal(err)
	}

	h := newPageHandlerAt(t, db, reposRoot)
	r := chi.NewRouter()
	r.Get("/{owner}/{repo}/checks", h.PageChecks)
	r.Get("/{owner}/{repo}/security/secrets", h.PageSecurityAlerts)
	r.Get("/{owner}/{repo}/security/secrets/{number}", h.PageSecurityAlert)
	r.Post("/{owner}/{repo}/security/secrets/{number}/close", h.CloseSecretAlert)
	r.Post("/{owner}/{repo}/security/secrets/{number}/reopen", h.ReopenSecretAlert)
	r.Post("/{owner}/{repo}/security/secrets/{number}/allow", h.AllowSecretAlert)
	return securityFixture{
		db: db, repo: repo, svc: svc, headSHA: head.Hash.String(),
		router: middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r),
	}
}

// record stores a finding on the fixture's commit and returns the alert number; kind is found or blocked.
func (f securityFixture) record(t *testing.T, typ, value, kind string, line int) *model.SecretScanAlert {
	t.Helper()
	finding := secretscan.Finding{Type: typ, Value: value, Line: line}
	a, _, err := f.svc.SecretAlert.Record(context.Background(), f.repo.id, finding, model.SecretSighting{
		CommitSHA: f.headSHA, FilePath: "config/deploy.env", Ref: "main", ActorID: f.repo.owner.id,
	}, kind)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return a
}

func (f securityFixture) alertPath(a *model.SecretScanAlert) string {
	return f.repo.path + "/security/secrets/" + strconv.Itoa(a.Number)
}

func (f securityFixture) user(t *testing.T, role model.Role) signedInUser {
	t.Helper()
	u := seedSignedInUser(t, f.db)
	grantRepoRole(t, f.db, f.repo.id, u.id, role)
	return u
}

func (f securityFixture) get(path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

func (f securityFixture) post(path, token string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

func awaitAudit(t *testing.T, db *sql.DB, action string, targetID int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, targetID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no audit_log entry %q for alert %d", action, targetID)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func eventKinds(t *testing.T, f securityFixture, alertID int64) []string {
	t.Helper()
	evs, err := f.svc.SecretAlert.Events(context.Background(), alertID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func TestSecurity_ReaderAndAnonymousGetTheMissingRepo404(t *testing.T) {
	for _, private := range []bool{false, true} {
		f := newSecurityFixture(t, private)
		alert := f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)
		reader := f.user(t, model.RoleReader)
		stranger := seedSignedInUser(t, f.db)

		type req struct{ method, suffix string }
		reqs := []req{
			{http.MethodGet, "/security/secrets"},
			{http.MethodGet, "/security/secrets/" + strconv.Itoa(alert.Number)},
			{http.MethodPost, "/security/secrets/" + strconv.Itoa(alert.Number) + "/close"},
			{http.MethodPost, "/security/secrets/" + strconv.Itoa(alert.Number) + "/reopen"},
			{http.MethodPost, "/security/secrets/" + strconv.Itoa(alert.Number) + "/allow"},
		}
		for _, who := range []struct{ name, token string }{{"anonymous", ""}, {"reader", reader.token}, {"stranger", stranger.token}} {
			for _, rq := range reqs {
				do := func(path string) *httptest.ResponseRecorder {
					if rq.method == http.MethodGet {
						return f.get(path, who.token)
					}
					return f.post(path, who.token, url.Values{"resolution": {"used_in_tests"}})
				}
				got := do(f.repo.path + rq.suffix)
				want := do("/nobody_" + testutil.UniqueSuffix(t) + "/norepo" + rq.suffix)
				if got.Code != http.StatusNotFound || want.Code != http.StatusNotFound {
					t.Errorf("private=%v %s %s %s: got %d, missing repo %d, want 404 both", private, who.name, rq.method, rq.suffix, got.Code, want.Code)
				}
				if got.Body.String() != want.Body.String() {
					t.Errorf("private=%v %s %s %s: body differs from the missing-repo 404", private, who.name, rq.method, rq.suffix)
				}
			}
		}
		reloaded, err := f.svc.SecretAlert.Get(context.Background(), f.repo.id, alert.Number)
		if err != nil || reloaded.State != model.SecretAlertOpen {
			t.Errorf("a refused POST changed the alert: %+v, %v", reloaded, err)
		}
	}
}

func TestSecurity_TabAndBadgeOnlyForWriters(t *testing.T) {
	f := newSecurityFixture(t, false)
	f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)
	// A blocked-only alert is open but no ref reaches it, so the badge leaves it out.
	f.record(t, "jwt_secret", "JWT_SECRET=aaaaaaaaaaaaaaaaaaaa", model.SecretEventBlocked, 3)
	writer := f.user(t, model.RoleWriter)
	reader := f.user(t, model.RoleReader)

	body := f.get(f.repo.path+"/checks", writer.token).Body.String()
	assertContains(t, body, `href="`+f.repo.path+`/security/secrets"`)
	assertContains(t, body, `aria-label="1 Security"`)

	for name, token := range map[string]string{"reader": reader.token, "anonymous": ""} {
		if b := f.get(f.repo.path+"/checks", token).Body.String(); strings.Contains(b, "/security/secrets") {
			t.Errorf("%s sees the Security tab", name)
		}
	}
}

func TestSecurity_ListCountsAndTypeFilterAgree(t *testing.T) {
	f := newSecurityFixture(t, false)
	f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)
	f.record(t, "generic_api_key", "api_key = 'k8s9fjw02nd8sk1pq7xz'", model.SecretEventFound, 1)
	closed := f.record(t, "generic_api_key", "api_key = 'zzzzzzzzzzzzzzzzzzzzzzzz'", model.SecretEventFound, 3)
	if err := f.svc.SecretAlert.Close(context.Background(), closed.ID, f.repo.owner.id, model.SecretResolutionRevoked); err != nil {
		t.Fatal(err)
	}
	owner := f.repo.owner.token

	open := f.get(f.repo.path+"/security/secrets", owner).Body.String()
	assertContains(t, open, "AWS access key ID")
	assertContains(t, open, "Generic API key")
	assertContains(t, open, `aria-label="2 items"`) // open tab
	assertContains(t, open, `aria-label="1 items"`) // closed tab
	if strings.Contains(open, "Revoked") {
		t.Error("the open tab lists a closed alert")
	}

	filtered := f.get(f.repo.path+"/security/secrets?type=aws_access_key_id", owner).Body.String()
	if strings.Contains(filtered, "Generic API key</a>") {
		t.Error("the type filter lists another type")
	}
	assertContains(t, filtered, `aria-label="1 items"`)
	assertContains(t, filtered, `aria-label="0 items"`)

	closedTab := f.get(f.repo.path+"/security/secrets?state=closed", owner).Body.String()
	assertContains(t, closedTab, "Revoked")
	assertContains(t, closedTab, "closed by "+f.repo.owner.name)
}

func TestSecurity_EmptyState(t *testing.T) {
	f := newSecurityFixture(t, false)
	body := f.get(f.repo.path+"/security/secrets", f.repo.owner.token).Body.String()
	assertContains(t, body, "No secrets found.")
}

func TestSecurity_CloseReopenAllowChangeStateAndWriteAuditAndEvents(t *testing.T) {
	f := newSecurityFixture(t, false)
	writer := f.user(t, model.RoleWriter)

	a := f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)
	rr := f.post(f.alertPath(a)+"/close", writer.token, url.Values{"resolution": {"revoked"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != f.alertPath(a) {
		t.Fatalf("close: %d %q", rr.Code, rr.Header().Get("Location"))
	}
	got, _ := f.svc.SecretAlert.Get(context.Background(), f.repo.id, a.Number)
	if got.State != model.SecretAlertClosed || got.Resolution != model.SecretResolutionRevoked {
		t.Errorf("after close: %+v", got)
	}
	awaitAudit(t, f.db, "secret_scan.close", a.ID)
	if page := f.get(f.alertPath(a), writer.token).Body.String(); !strings.Contains(page, "Closed · Revoked") || !strings.Contains(page, "Reopen") {
		t.Error("closed alert page lacks the Closed badge or Reopen")
	}

	if rr := f.post(f.alertPath(a)+"/close", writer.token, url.Values{"resolution": {"revoked"}}); rr.Code != http.StatusConflict {
		t.Errorf("closing a closed alert: %d, want 409", rr.Code)
	}

	if rr := f.post(f.alertPath(a)+"/reopen", writer.token, nil); rr.Code != http.StatusSeeOther {
		t.Fatalf("reopen: %d", rr.Code)
	}
	awaitAudit(t, f.db, "secret_scan.reopen", a.ID)
	got, _ = f.svc.SecretAlert.Get(context.Background(), f.repo.id, a.Number)
	if got.State != model.SecretAlertOpen || got.Resolution != "" {
		t.Errorf("after reopen: %+v", got)
	}

	blocked := f.record(t, "jwt_secret", "JWT_SECRET=aaaaaaaaaaaaaaaaaaaa", model.SecretEventBlocked, 3)
	if rr := f.post(f.alertPath(blocked)+"/allow", writer.token, url.Values{"resolution": {"used_in_tests"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("allow: %d %s", rr.Code, rr.Body.String())
	}
	awaitAudit(t, f.db, "secret_scan.allow", blocked.ID)
	got, _ = f.svc.SecretAlert.Get(context.Background(), f.repo.id, blocked.Number)
	if got.State != model.SecretAlertClosed || got.Resolution != model.SecretResolutionUsedInTests {
		t.Errorf("after allow: %+v", got)
	}

	if kinds := strings.Join(eventKinds(t, f, a.ID), ","); kinds != "found,closed,reopened" {
		t.Errorf("events = %s", kinds)
	}
	if kinds := strings.Join(eventKinds(t, f, blocked.ID), ","); kinds != "blocked,allowed" {
		t.Errorf("events = %s", kinds)
	}
}

func TestSecurity_AllowNotifiesAdminsButNotTheAllower(t *testing.T) {
	f := newSecurityFixture(t, false)
	writer := f.user(t, model.RoleWriter)
	a := f.record(t, "jwt_secret", "JWT_SECRET=aaaaaaaaaaaaaaaaaaaa", model.SecretEventBlocked, 3)

	if rr := f.post(f.alertPath(a)+"/allow", writer.token, url.Values{"resolution": {"false_positive"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("allow: %d %s", rr.Code, rr.Body.String())
	}

	ns, err := f.svc.Notification.List(context.Background(), f.repo.owner.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Type != model.NotifSecretAllowed || ns[0].SubjectID != int64(a.Number) {
		t.Errorf("owner notifications = %+v, want one secret_allowed for alert %d", ns, a.Number)
	}
	if got, _ := f.svc.Notification.List(context.Background(), writer.id); len(got) != 0 {
		t.Errorf("the allower got %d notifications, want 0", len(got))
	}
}

func TestSecurity_AllowRefusedForCloudzillaToken(t *testing.T) {
	f := newSecurityFixture(t, false)
	token := "czp_" + strings.Repeat("ab", 32)
	a := f.record(t, "cloudzilla_token", token, model.SecretEventBlocked, 3)

	rr := f.post(f.alertPath(a)+"/allow", f.repo.owner.token, url.Values{"resolution": {"false_positive"}})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("allow on a token: %d, want 422", rr.Code)
	}
	got, _ := f.svc.SecretAlert.Get(context.Background(), f.repo.id, a.Number)
	if got.State != model.SecretAlertOpen {
		t.Errorf("alert left open state: %+v", got)
	}
	page := f.get(f.alertPath(a), f.repo.owner.token).Body.String()
	if strings.Contains(page, "/allow") {
		t.Error("a token alert page offers Allow")
	}
}

func TestSecurity_BadResolutionIsRefused(t *testing.T) {
	f := newSecurityFixture(t, false)
	a := f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)
	if rr := f.post(f.alertPath(a)+"/close", f.repo.owner.token, url.Values{"resolution": {"nope"}}); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown resolution: %d, want 400", rr.Code)
	}
}

func TestSecurity_AlertPageMasksTheSecret(t *testing.T) {
	f := newSecurityFixture(t, false)
	a := f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)
	owner := f.repo.owner.token

	for name, rr := range map[string]*httptest.ResponseRecorder{
		"page": f.get(f.alertPath(a), owner),
		"list": f.get(f.repo.path+"/security/secrets", owner),
	} {
		if strings.Contains(rr.Body.String(), securityTestKey) {
			t.Errorf("%s holds the full secret", name)
		}
	}
	page := f.get(f.alertPath(a), owner).Body.String()
	assertContains(t, page, "AWS_ACCESS_KEY_ID=")
	assertContains(t, page, "AKIA••••MPLE")
	assertContains(t, page, "AKIA…MPLE")
	assertContains(t, page, "View file at "+f.headSHA[:7])
	assertContains(t, page, "Deactivate the key in AWS IAM")

	closedPost := f.post(f.alertPath(a)+"/close", owner, url.Values{"resolution": {"revoked"}})
	if strings.Contains(closedPost.Body.String(), securityTestKey) {
		t.Error("close response holds the full secret")
	}
}

func TestSecurity_PrunedCommitSaysSo(t *testing.T) {
	f := newSecurityFixture(t, false)
	f.headSHA = strings.Repeat("ab", 20)
	a := f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventFound, 2)

	rr := f.get(f.alertPath(a), f.repo.owner.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("pruned alert page: %d", rr.Code)
	}
	assertContains(t, rr.Body.String(), "Commit no longer available")
	assertContains(t, rr.Body.String(), "AKIA…MPLE")
}

func TestSecurity_BlockedAlertShowsBannerAndDefaultBranchStatus(t *testing.T) {
	f := newSecurityFixture(t, false)
	realHead := f.headSHA
	f.headSHA = strings.Repeat("cd", 20) // a blocked push's commit is on no ref
	a := f.record(t, "aws_access_key_id", securityTestKey, model.SecretEventBlocked, 2)
	f.headSHA = realHead

	page := f.get(f.alertPath(a), f.repo.owner.token).Body.String()
	assertContains(t, page, "This secret blocked a push")
	assertContains(t, page, "Allow as used in tests")
	assertContains(t, page, "Allow as false positive")
	assertContains(t, page, "Blocked push")
	assertContains(t, page, `<p class="text-[13px]">No</p>`)

	reached := f.record(t, "jwt_secret", "JWT_SECRET=aaaaaaaaaaaaaaaaaaaa", model.SecretEventFound, 3)
	if err := f.svc.SecretAlert.MarkReached(context.Background(), f.repo.id, []string{f.headSHA}); err != nil {
		t.Fatal(err)
	}
	page = f.get(f.alertPath(reached), f.repo.owner.token).Body.String()
	assertContains(t, page, "Yes, in")
}

func TestSecurity_UnknownAlertNumberIs404(t *testing.T) {
	f := newSecurityFixture(t, false)
	for _, path := range []string{f.repo.path + "/security/secrets/999", f.repo.path + "/security/secrets/abc"} {
		if rr := f.get(path, f.repo.owner.token); rr.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rr.Code)
		}
	}
}
