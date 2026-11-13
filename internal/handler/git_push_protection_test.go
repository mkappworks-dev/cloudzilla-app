package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const awsKey = "AKIAIOSFODNN7EXAMPLE"

type pushEnv struct {
	h     http.Handler
	svcs  *service.Services
	r     raceRepo
	calls atomic.Int32
	posts atomic.Int32
	notes *recordingNotifier
}

// waitPostReceive waits until n scans after a push have finished, so none is still writing when the test's rows go.
func (e *pushEnv) waitPostReceive(t *testing.T, n int32) {
	t.Helper()
	eventually(t, "the scan after the push", func() bool { return e.posts.Load() >= n })
}

type recordingNotifier struct {
	mu     sync.Mutex
	alerts []*model.SecretScanAlert
}

func (n *recordingNotifier) NotifySecretAlert(_ context.Context, _ *model.Repository, a *model.SecretScanAlert) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.alerts = append(n.alerts, a)
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.alerts)
}

// newPushEnv serves a seeded repo with push protection on; tune edits the secret scanning config first.
func newPushEnv(t *testing.T, db *sql.DB, tune func(*config.SecretScanningConfig)) *pushEnv {
	t.Helper()
	reposRoot := t.TempDir()
	sc := config.SecretScanningConfig{PushProtection: true, MaxBlobBytes: 1 << 20, PushTimeout: 10 * time.Second, MaxBlobs: 5000}
	if tune != nil {
		tune(&sc)
	}
	cfg := &config.Config{
		Server:         config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:           config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:            config.GitConfig{ReposRoot: reposRoot},
		SecretScanning: sc,
	}
	svcs := service.New(store.New(db), cfg)
	h, err := router.New(svcs, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	e := &pushEnv{h: h, svcs: svcs, r: seedRaceRepo(t, db, reposRoot), notes: &recordingNotifier{}}
	svcs.PushScan.WithNotifier(e.notes).WithPostReceiveDone(func() { e.posts.Add(1) }).WithScanner(func(path string, content []byte, max int) ([]secretscan.Finding, error) {
		e.calls.Add(1)
		return secretscan.Scan(path, content, max), nil
	})
	return e
}

func (e *pushEnv) wantRefusal(number int) string {
	return "secret detected: aws_access_key_id in config/deploy.env:1; if it is not a live secret, a writer can allow it at " +
		"http://localhost:8080" + e.r.path + "/security/secrets/" + strconv.Itoa(number)
}

func (e *pushEnv) push(t *testing.T, c *testutil.PushClient, cmds ...*packp.Command) map[plumbing.ReferenceName]string {
	t.Helper()
	return receivePackOf(t, e.h, e.r, c.Pack(), cmds...)
}

func (e *pushEnv) alerts(t *testing.T) []model.SecretScanAlert {
	t.Helper()
	got, _, err := e.svcs.SecretAlert.List(context.Background(), e.r.id, "", "", 1)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	return got
}

func (e *pushEnv) locations(t *testing.T, alertID int64) []model.SecretScanLocation {
	t.Helper()
	got, err := e.svcs.SecretAlert.Locations(context.Background(), alertID)
	if err != nil {
		t.Fatalf("locations: %v", err)
	}
	return got
}

// eventually waits for the scan after the push, which runs in the background.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func captureSlog(t *testing.T) *lockedBuffer {
	t.Helper()
	var buf lockedBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func create(ref plumbing.ReferenceName, to plumbing.Hash) *packp.Command {
	return &packp.Command{Name: ref, New: to}
}

func keyFile() map[string]string {
	return map[string]string{"config/deploy.env": "AWS_ACCESS_KEY_ID=" + awsKey + "\n"}
}

func TestPushProtection_NewBranchWithKey_RefusedAndAlerted(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	bad := c.Commit(e.r.mainTip, keyFile(), "add key")

	refs := e.push(t, c, create(topicRef, bad))

	if got, want := refs[topicRef], e.wantRefusal(1); got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	if _, err := e.r.git.Reference(topicRef, true); !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Errorf("the refused ref exists: %v", err)
	}
	alerts := e.alerts(t)
	if len(alerts) != 1 || !alerts[0].Blocked || alerts[0].SecretType != "aws_access_key_id" {
		t.Fatalf("alerts = %+v, want one blocked aws_access_key_id", alerts)
	}
	locs := e.locations(t, alerts[0].ID)
	if len(locs) != 1 || locs[0].ReachedRef || locs[0].FilePath != "config/deploy.env" || locs[0].LineNumber != 1 || locs[0].CommitSHA != bad.String() {
		t.Errorf("locations = %+v", locs)
	}
	time.Sleep(100 * time.Millisecond)
	if n := e.notes.count(); n != 0 {
		t.Errorf("a blocked push notified %d times", n)
	}
}

func TestPushProtection_SecondRefSharingTheCommit_RefusedAndScannedOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	bad := c.Commit(e.r.mainTip, keyFile(), "add key")

	refs := e.push(t, c, create(topicRef, bad), create(plumbing.NewBranchReferenceName("topic2"), bad))

	for ref, got := range refs {
		if want := e.wantRefusal(1); got != want {
			t.Errorf("%s status = %q, want %q", ref, got, want)
		}
	}
	if len(refs) != 2 {
		t.Errorf("statuses = %v, want two", refs)
	}
	if n := e.calls.Load(); n != 1 {
		t.Errorf("scanner ran %d times, want once for the one changed file", n)
	}
	if n := len(e.alerts(t)); n != 1 {
		t.Errorf("%d alerts, want 1", n)
	}
}

func TestPushProtection_FastForwardAddingKey_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	bad := c.Commit(e.r.mainTip, keyFile(), "add key")

	refs := e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: bad})

	if got, want := refs[mainRef], e.wantRefusal(1); got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	assertRef(t, e.r, "main", e.r.mainTip)
}

func TestPushProtection_AnnotatedTagOfCommitAddingKey_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	tag := c.Tag(c.Commit(e.r.mainTip, keyFile(), "add key"), "v1")
	tagRef := plumbing.NewTagReferenceName("v1")

	refs := e.push(t, c, create(tagRef, tag))

	if got, want := refs[tagRef], e.wantRefusal(1); got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	if _, err := e.r.git.Reference(tagRef, true); !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Errorf("the refused tag exists: %v", err)
	}
}

func TestPushProtection_ForcePushAddingKey_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	// Rooted off feature's tip, so main is rewound onto a history that doesn't hold it.
	bad := c.Commit(e.r.featureTip, keyFile(), "add key")

	refs := e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: bad})

	if got, want := refs[mainRef], e.wantRefusal(1); got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	assertRef(t, e.r, "main", e.r.mainTip)
}

func TestPushProtection_ManyFindings_SaysHowManyMore(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	files := keyFile()
	files["z.env"] = "AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE\n"
	bad := c.Commit(e.r.mainTip, files, "add keys")

	refs := e.push(t, c, create(topicRef, bad))

	want := e.wantRefusal(1)
	want = strings.Replace(want, "deploy.env:1;", "deploy.env:1 and 1 more;", 1)
	if got := refs[topicRef]; got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
}

func TestPushProtection_NothingToBlock_Passes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	clean := c.Commit(e.r.mainTip, map[string]string{"readme.md": "hello\n"}, "clean")

	refs := e.push(t, c, create(topicRef, clean), &packp.Command{Name: plumbing.NewBranchReferenceName("feature"), Old: e.r.featureTip})

	for ref, got := range refs {
		if got != "ok" {
			t.Errorf("%s status = %q, want ok", ref, got)
		}
	}
	assertRef(t, e.r, "topic", clean)
	if n := len(e.alerts(t)); n != 0 {
		t.Errorf("%d alerts for a clean push", n)
	}
}

func TestPushProtection_DeleteOnlyPush_Passes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)

	refs := e.push(t, c, &packp.Command{Name: plumbing.NewBranchReferenceName("feature"), Old: e.r.featureTip})

	if got := refs[plumbing.NewBranchReferenceName("feature")]; got != "ok" {
		t.Errorf("status = %q, want ok", got)
	}
	if n := e.calls.Load(); n != 0 {
		t.Errorf("a delete scanned %d blobs", n)
	}
}

func TestPushProtection_ClosedAlert_PassesAndReopenedBlocksAgain(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	bad := c.Commit(e.r.mainTip, keyFile(), "add key")
	if got := e.push(t, c, create(topicRef, bad))[topicRef]; got != e.wantRefusal(1) {
		t.Fatalf("first push status = %q", got)
	}
	alert := e.alerts(t)[0]
	ctx := context.Background()

	if err := e.svcs.SecretAlert.Allow(ctx, alert.ID, e.r.owner.id, model.SecretResolutionUsedInTests); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if got := e.push(t, c, create(topicRef, bad))[topicRef]; got != "ok" {
		t.Fatalf("retry after allow status = %q, want ok", got)
	}
	assertRef(t, e.r, "topic", bad)
	eventually(t, "the location to be appended and reached", func() bool {
		locs := e.locations(t, alert.ID)
		return len(locs) == 1 && locs[0].ReachedRef
	})
	if n := e.notes.count(); n != 0 {
		t.Errorf("an allowed value notified %d times", n)
	}

	if err := e.svcs.SecretAlert.Reopen(ctx, alert.ID, e.r.owner.id); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	other := c.Commit(bad, map[string]string{"other.txt": "x\n", "config/deploy2.env": "# copy\nAWS_ACCESS_KEY_ID=" + awsKey + "\n"}, "again")
	want := strings.Replace(strings.Replace(e.wantRefusal(1), "deploy.env", "deploy2.env", 1), "env:1", "env:2", 1)
	if got := e.push(t, c, create(plumbing.NewBranchReferenceName("topic2"), other))[plumbing.NewBranchReferenceName("topic2")]; got != want {
		t.Errorf("after reopen status = %q, want %q", got, want)
	}
}

func TestPushProtection_ClosedAlertValue_NewLocationAppended(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	first := c.Commit(e.r.mainTip, keyFile(), "add key")
	e.push(t, c, create(topicRef, first))
	alert := e.alerts(t)[0]
	if err := e.svcs.SecretAlert.Allow(context.Background(), alert.ID, e.r.owner.id, model.SecretResolutionFalsePositive); err != nil {
		t.Fatalf("allow: %v", err)
	}
	second := c.Commit(e.r.mainTip, map[string]string{"tests/fixture.env": "KEY=" + awsKey + "\n"}, "fixture")

	if got := e.push(t, c, create(plumbing.NewBranchReferenceName("fixtures"), second))[plumbing.NewBranchReferenceName("fixtures")]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	eventually(t, "the second location", func() bool { return len(e.locations(t, alert.ID)) == 2 })
	if got := e.alerts(t); len(got) != 1 || got[0].State != model.SecretAlertClosed {
		t.Errorf("alerts = %+v, want the one closed alert", got)
	}
}

func TestPushProtection_BudgetExhausted_PassesAndAlertsAfterPush(t *testing.T) {
	twoFiles := map[string]string{"a.txt": "nothing here\n", "b.env": "AWS_ACCESS_KEY_ID=" + awsKey + "\n"}
	for _, tc := range []struct {
		name string
		tune func(*config.SecretScanningConfig)
		scan func(*pushEnv) service.SecretScanFunc
	}{
		{"timeout", func(c *config.SecretScanningConfig) { c.PushTimeout = time.Millisecond }, func(e *pushEnv) service.SecretScanFunc {
			var first atomic.Bool
			return func(path string, content []byte, max int) ([]secretscan.Finding, error) {
				if first.CompareAndSwap(false, true) {
					time.Sleep(30 * time.Millisecond)
				}
				return secretscan.Scan(path, content, max), nil
			}
		}},
		{"blob cap", func(c *config.SecretScanningConfig) { c.MaxBlobs = 1 }, nil},
		{"scanner error", nil, func(e *pushEnv) service.SecretScanFunc {
			var first atomic.Bool
			return func(path string, content []byte, max int) ([]secretscan.Finding, error) {
				if first.CompareAndSwap(false, true) {
					return nil, errors.New("scanner crashed")
				}
				return secretscan.Scan(path, content, max), nil
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			logs := captureSlog(t)
			e := newPushEnv(t, db, tc.tune)
			if tc.scan != nil {
				e.svcs.PushScan.WithScanner(tc.scan(e))
			}
			c := testutil.NewPushClient(t, e.r.git)
			tip := c.Commit(e.r.mainTip, twoFiles, "add key")

			refs := e.push(t, c, create(topicRef, tip))

			if got := refs[topicRef]; got != "ok" {
				t.Fatalf("status = %q, want ok", got)
			}
			assertRef(t, e.r, "topic", tip)
			if !strings.Contains(logs.String(), "push-protection: scan incomplete") {
				t.Errorf("no incomplete-scan log:\n%s", logs.String())
			}
			eventually(t, "the alert after the push", func() bool { return len(e.alerts(t)) == 1 })
			a := e.alerts(t)[0]
			if !a.Incomplete || a.Blocked {
				t.Errorf("alert = %+v, want incomplete and reached", a)
			}
			eventually(t, "the notification", func() bool { return e.notes.count() == 1 })
		})
	}
}

func TestPushProtection_Off_PassesAndAlertsAfterPush(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, func(c *config.SecretScanningConfig) { c.PushProtection = false })
	c := testutil.NewPushClient(t, e.r.git)
	bad := c.Commit(e.r.mainTip, keyFile(), "add key")

	refs := e.push(t, c, create(topicRef, bad))

	if got := refs[topicRef]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	eventually(t, "the alert after the push", func() bool { return len(e.alerts(t)) == 1 })
	a := e.alerts(t)[0]
	if a.Incomplete || a.Blocked {
		t.Errorf("alert = %+v, want complete and reached", a)
	}
	eventually(t, "the notification", func() bool { return e.notes.count() == 1 })
}

func TestPushProtection_GenericKey_PassesAndAlertsAfterPush(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	tip := c.Commit(e.r.mainTip, map[string]string{"app.py": `api_key = "k8Zp2Qw9xLmN4vB7tR1yH6uJ"` + "\n"}, "add key")

	refs := e.push(t, c, create(topicRef, tip))

	if got := refs[topicRef]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	eventually(t, "the alert after the push", func() bool { return len(e.alerts(t)) == 1 })
	if a := e.alerts(t)[0]; a.SecretType != "generic_api_key" || a.Blocked {
		t.Errorf("alert = %+v, want a reached generic_api_key", a)
	}
}

func TestPushProtection_DeadCloudzillaToken_Passes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	tip := c.Commit(e.r.mainTip, map[string]string{"t.env": "TOKEN=czp_" + strings.Repeat("ab", 32) + "\n"}, "add token")

	refs := e.push(t, c, create(topicRef, tip))

	if got := refs[topicRef]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(e.alerts(t)); n != 0 {
		t.Errorf("%d alerts for a token no stored hash matches", n)
	}
}

func TestPushProtection_LiveCloudzillaToken_BlockedThroughResolver(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	e.svcs.PushScan.WithTokenResolver(liveTokens{})
	c := testutil.NewPushClient(t, e.r.git)
	tip := c.Commit(e.r.mainTip, map[string]string{"t.env": "TOKEN=czp_" + strings.Repeat("ab", 32) + "\n"}, "add token")

	refs := e.push(t, c, create(topicRef, tip))

	if got := refs[topicRef]; !strings.HasPrefix(got, "secret detected: cloudzilla_token in t.env:1") {
		t.Errorf("status = %q, want a cloudzilla_token refusal", got)
	}
}

type liveTokens struct{}

func (liveTokens) ResolveLiveToken(context.Context, *model.Repository, string) (bool, error) {
	return true, nil
}
