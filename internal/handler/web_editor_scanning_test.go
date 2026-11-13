package handler_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const keyLine = "AWS_ACCESS_KEY_ID=" + awsKey + "\n"

func (e *pushEnv) editA(t *testing.T, content string) (code int, body string, head string) {
	t.Helper()
	f, err := e.svcs.Code.GetBranchFile(e.r.owner.name, e.r.name, "main", "a.txt", 1<<20)
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	rr := send(t, e.h, http.MethodPost, e.r.owner.token, e.r.path+"/edit/main/a.txt",
		url.Values{"path": {"a.txt"}, "content": {content}, "blob_sha": {f.SHA}}, false)
	return rr.Code, rr.Body.String(), branchHash(t, e.r.git, "main").String()
}

func TestWebEditor_EditAddingAKey_RefusedThenAllowedOnceClosed(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	before := e.r.mainTip.String()

	code, body, head := e.editA(t, "one\n"+keyLine)

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %.300s", code, body)
	}
	for _, want := range []string{"secret detected: aws_access_key_id in a.txt:2", "/security/secrets/1", awsKey} {
		if !strings.Contains(body, want) {
			t.Errorf("form lacks %q", want)
		}
	}
	if head != before {
		t.Errorf("main moved to %s", head)
	}
	alerts := e.alerts(t)
	if len(alerts) != 1 || !alerts[0].Blocked {
		t.Fatalf("alerts = %+v, want one blocked", alerts)
	}

	if err := e.svcs.SecretAlert.Close(context.Background(), alerts[0].ID, e.r.owner.id, model.SecretResolutionUsedInTests); err != nil {
		t.Fatalf("close: %v", err)
	}
	if code, body, _ = e.editA(t, "one\n"+keyLine); code != http.StatusSeeOther {
		t.Fatalf("after closing: status = %d, want 303: %.300s", code, body)
	}
}

func TestWebEditor_NewFileWithAKey_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)

	rr := send(t, e.h, http.MethodPost, e.r.owner.token, e.r.path+"/new/main",
		url.Values{"path": {"deploy.env"}, "content": {keyLine}}, false)

	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "secret detected: aws_access_key_id in deploy.env:1") {
		t.Fatalf("got %d: %.300s", rr.Code, rr.Body.String())
	}
	if branchHash(t, e.r.git, "main") != e.r.mainTip {
		t.Error("main moved")
	}
}

func TestWebEditor_RenameWithoutNewContentIsNotScanned(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	f, err := e.svcs.Code.GetBranchFile(e.r.owner.name, e.r.name, "main", "a.txt", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	before := e.calls.Load()

	rr := send(t, e.h, http.MethodPost, e.r.owner.token, e.r.path+"/edit/main/a.txt",
		url.Values{"path": {"b.txt"}, "content": {string(f.Content)}, "blob_sha": {f.SHA}}, false)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %.300s", rr.Code, rr.Body.String())
	}
	if n := e.calls.Load() - before; n != 0 {
		t.Errorf("scanner ran %d times for a pure rename", n)
	}
}

func TestWebEditor_ProtectionOff_CommitsAndAlertsAfter(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, func(c *config.SecretScanningConfig) { c.PushProtection = false })

	code, body, _ := e.editA(t, "one\n"+keyLine)

	if code != http.StatusSeeOther {
		t.Fatalf("status = %d: %.300s", code, body)
	}
	eventually(t, "the alert after the web commit", func() bool { return len(e.alerts(t)) == 1 })
}
