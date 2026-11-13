package handler_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

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
