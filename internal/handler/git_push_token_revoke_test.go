package handler_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func tokenFile(raw string) map[string]string {
	return map[string]string{"scripts/smoke.sh": "TOKEN=" + raw + "\n"}
}

func onlyAlert(t *testing.T, e *pushEnv) model.SecretScanAlert {
	t.Helper()
	as := e.alerts(t)
	if len(as) != 1 {
		t.Fatalf("%d alerts, want 1", len(as))
	}
	return as[0]
}

func revokeAudits(t *testing.T, db *sql.DB, userID int64) (n int) {
	t.Helper()
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = $1 AND target_id = $2`,
		model.AuditActionTokenAutoRevoke, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func newOwnerToken(t *testing.T, e *pushEnv, db *sql.DB) (int64, string, *model.AccessToken) {
	t.Helper()
	ownerID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	raw, tok, err := e.svcs.AccessToken.Generate(context.Background(), ownerID, "ci", []string{"repo:read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ownerID, raw, tok
}

func TestPushProtection_LiveToken_RevokedAndRefused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	ownerID, raw, tok := newOwnerToken(t, e, db)
	c := testutil.NewPushClient(t, e.r.git)
	tip := c.Commit(e.r.mainTip, tokenFile(raw), "add token")

	refs := e.push(t, c, create(topicRef, tip))

	want := "secret detected: Cloudzilla access token in scripts/smoke.sh:1 (the token was revoked); see "
	if got := refs[topicRef]; !strings.HasPrefix(got, want) || strings.Contains(got, "allow") {
		t.Errorf("status = %q, want prefix %q and no allow link", got, want)
	}
	if _, _, err := e.svcs.AccessToken.Validate(context.Background(), raw); err == nil {
		t.Error("the leaked token still validates")
	}
	a := onlyAlert(t, e)
	if a.State != model.SecretAlertClosed || !a.AutoRevoked || a.Resolution != model.SecretResolutionRevoked {
		t.Errorf("alert = %+v, want closed revoked auto_revoked", a)
	}
	if n := revokeAudits(t, db, ownerID); n != 1 {
		t.Errorf("%d audit entries, want 1", n)
	}
	ns, err := e.svcs.Notification.List(context.Background(), ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Type != model.NotifTokenRevoked || ns[0].SubjectID != tok.ID {
		t.Errorf("notifications = %+v, want one token_revoked", ns)
	}
}

func TestPushProtection_UnknownToken_NoAlert(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	c := testutil.NewPushClient(t, e.r.git)
	tip := c.Commit(e.r.mainTip, tokenFile("czp_"+strings.Repeat("cd", 32)), "add token")

	refs := e.push(t, c, create(topicRef, tip))

	if got := refs[topicRef]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	e.waitPostReceive(t, 1)
	if n := len(e.alerts(t)); n != 0 {
		t.Errorf("%d alerts, want 0", n)
	}
}

func TestPushProtection_ExpiredToken_ClosedAlertNoRevoke(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	ownerID, raw, tok := newOwnerToken(t, e, db)
	testutil.Exec(t, db, `UPDATE access_tokens SET expires_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, tok.ID)
	c := testutil.NewPushClient(t, e.r.git)
	tip := c.Commit(e.r.mainTip, tokenFile(raw), "add token")

	refs := e.push(t, c, create(topicRef, tip))

	if got := refs[topicRef]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	e.waitPostReceive(t, 1)
	a := onlyAlert(t, e)
	if a.State != model.SecretAlertClosed || a.AutoRevoked || a.Resolution != model.SecretResolutionRevoked {
		t.Errorf("alert = %+v, want closed revoked, not auto_revoked", a)
	}
	if got, err := store.New(db).AccessToken.GetByHash(context.Background(), tok.TokenHash); err != nil || got.ID != tok.ID {
		t.Errorf("expired token was deleted: %v", err)
	}
	if n := revokeAudits(t, db, ownerID); n != 0 {
		t.Errorf("%d audit entries, want 0", n)
	}
}

func TestPushProtection_RevokedTokenSeenAgain_AppendsLocation(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	ownerID, raw, _ := newOwnerToken(t, e, db)
	c := testutil.NewPushClient(t, e.r.git)
	e.push(t, c, create(topicRef, c.Commit(e.r.mainTip, tokenFile(raw), "first")))
	a := onlyAlert(t, e)
	before := len(e.locations(t, a.ID))

	again := plumbing.NewBranchReferenceName("again")
	c2 := testutil.NewPushClient(t, e.r.git)
	tip := c2.Commit(e.r.mainTip, map[string]string{"other.sh": "T=" + raw + "\n"}, "again")
	refs := e.push(t, c2, create(again, tip))

	if got := refs[again]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	e.waitPostReceive(t, 2)
	if len(e.locations(t, a.ID)) <= before {
		t.Error("the second location was not appended")
	}
	evs, err := e.svcs.SecretAlert.Events(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range evs {
		kinds = append(kinds, ev.Kind)
	}
	if got := strings.Join(kinds, ","); got != "blocked,auto_revoked,found" {
		t.Errorf("events = %s, want the check to add none of its own", got)
	}
	if n := revokeAudits(t, db, ownerID); n != 1 {
		t.Errorf("%d audit entries, want 1", n)
	}
}
