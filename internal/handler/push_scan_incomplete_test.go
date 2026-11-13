package handler_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestPushProtection_RefReachesBlockedAlert_NotifiesAdminsOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	bad := c.Commit(e.r.mainTip, keyFile(), "add key")
	if got := e.push(t, c, create(topicRef, bad))[topicRef]; got != e.wantRefusal(1) {
		t.Fatalf("first push status = %q", got)
	}
	if n := e.notes.count(); n != 0 {
		t.Fatalf("a blocked push notified %d times", n)
	}

	var failed atomic.Bool
	e.svcs.PushScan.WithScanner(func(path string, content []byte, max int) ([]secretscan.Finding, error) {
		if failed.CompareAndSwap(false, true) {
			return nil, errors.New("scanner crashed")
		}
		return secretscan.Scan(path, content, max), nil
	})
	other := plumbing.NewBranchReferenceName("other")
	if got := e.push(t, c, create(other, bad))[other]; got != "ok" {
		t.Fatalf("second push status = %q, want ok", got)
	}
	e.waitPostReceive(t, 2)

	if n := e.notes.count(); n != 1 {
		t.Errorf("admins notified %d times when a ref reached the blocked alert, want 1", n)
	}
	if a := e.alerts(t)[0]; a.Blocked {
		t.Errorf("alert = %+v, want reached", a)
	}
}

func TestPushProtection_IncompleteScan_OnlyUnscannedCommitsAreFlagged(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, func(c *config.SecretScanningConfig) { c.MaxBlobs = 1 })
	c := testutil.NewPushClient(t, e.r.git)
	scanned := c.Commit(e.r.mainTip, map[string]string{"app.py": `api_key = "k8Zp2Qw9xLmN4vB7tR1yH6uJ"` + "\n"}, "generic key")
	unscanned := c.Commit(scanned, keyFile(), "aws key")

	refs := e.push(t, c, create(topicRef, unscanned))

	if got := refs[topicRef]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	e.waitPostReceive(t, 1)
	byType := map[string]bool{}
	for _, a := range e.alerts(t) {
		byType[a.SecretType] = a.Incomplete
	}
	if len(byType) != 2 {
		t.Fatalf("alerts = %v, want one generic_api_key and one aws_access_key_id", byType)
	}
	if byType["generic_api_key"] {
		t.Error("the finding of a commit the scan finished is flagged incomplete")
	}
	if !byType["aws_access_key_id"] {
		t.Error("the finding of a commit the scan did not reach is not flagged incomplete")
	}
}
