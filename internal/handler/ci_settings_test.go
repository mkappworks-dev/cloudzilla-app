package handler_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestCISettings_ManagerSavesAWriteOnlySecretAndNobodyReadsItBack(t *testing.T) {
	e := newActionsEnv(t)
	save := func(token string, name, value string) *httpResult {
		rr := e.req(http.MethodPost, e.r.path+"/settings/secrets", token, url.Values{"name": {name}, "value": {value}})
		return &httpResult{rr.Code, rr.Body.String(), rr.Header()}
	}
	if r := save(e.writer.token, "DEPLOY_TOKEN", "hunter2-secret"); r.code != http.StatusForbidden {
		t.Fatalf("a writer saved a secret: %d", r.code)
	}
	if r := save(e.reader.token, "DEPLOY_TOKEN", "x"); r.code != http.StatusForbidden {
		t.Fatalf("a reader saved a secret: %d", r.code)
	}

	r := save(e.r.owner.token, "DEPLOY_TOKEN", "hunter2-secret")
	if r.code != http.StatusOK {
		t.Fatalf("manager save = %d: %s", r.code, r.body)
	}
	mustContain(t, r.body, "DEPLOY_TOKEN", `id="secrets-card"`, "Replace")
	mustNotContain(t, r.body, "hunter2-secret")

	page := e.get(t, e.r.path+"/settings", e.r.owner.token, http.StatusOK)
	mustContain(t, page, "DEPLOY_TOKEN", "Enable CI for this repository", "Secrets", `id="secret-dialog"`)
	mustNotContain(t, page, "hunter2-secret")

	// Replacing keeps one row; a bad name is refused with its reason and keeps the form's slot.
	if r := save(e.r.owner.token, "DEPLOY_TOKEN", "second-value"); r.code != http.StatusOK {
		t.Fatalf("replace = %d", r.code)
	}
	repo, _ := e.svcs.Repo.GetByID(context.Background(), e.r.id)
	if list, _ := e.svcs.CISecret.List(context.Background(), repo.ID); len(list) != 1 {
		t.Errorf("secrets = %+v", list)
	}
	bad := save(e.r.owner.token, "1 bad", "v")
	if bad.header.Get("HX-Retarget") != "#secret-form-error" {
		t.Errorf("bad name: %d %v", bad.code, bad.header)
	}
	mustContain(t, bad.body, "a secret name is letters")
	mustNotContain(t, bad.body, "v</div>")

	del := e.req(http.MethodPost, e.r.path+"/settings/secrets/DEPLOY_TOKEN/delete", e.r.owner.token, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("delete = %d", del.Code)
	}
	mustContain(t, del.Body.String(), "No secrets yet.")
	if rr := e.req(http.MethodPost, e.r.path+"/settings/secrets/DEPLOY_TOKEN/delete", e.writer.token, nil); rr.Code != http.StatusForbidden {
		t.Errorf("writer delete = %d", rr.Code)
	}
}

type httpResult struct {
	code   int
	body   string
	header http.Header
}

func TestCISettings_NoSecretKeyRefusesWithAMessageAndDisablesTheButtons(t *testing.T) {
	e := newCIPushEnv(t)
	a := &actionsEnv{runnerEnv: &runnerEnv{ciPushEnv: e}}
	rr := a.req(http.MethodPost, e.r.path+"/settings/secrets", e.r.owner.token, url.Values{"name": {"TOKEN"}, "value": {"v"}})
	if rr.Header().Get("HX-Retarget") != "#secret-form-error" {
		t.Fatalf("not refused in the form slot: %d %v", rr.Code, rr.Header())
	}
	mustContain(t, rr.Body.String(), "secrets need security.secret_key")
	page := a.get(t, e.r.path+"/settings", e.r.owner.token, http.StatusOK)
	mustContain(t, page, "Set security.secret_key to store secrets")
}

func TestCISettings_ManagerTurnsRepoCIOffAndOn(t *testing.T) {
	e := newActionsEnv(t)
	on := func() bool {
		v, err := e.svcs.WorkflowRun.RepoCIEnabled(context.Background(), e.r.id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if rr := e.req(http.MethodPost, e.r.path+"/settings/ci", e.writer.token, url.Values{}); rr.Code != http.StatusForbidden {
		t.Fatalf("writer toggled CI: %d", rr.Code)
	}
	if rr := e.req(http.MethodPost, e.r.path+"/settings/ci", e.r.owner.token, url.Values{}); rr.Code >= 400 || on() {
		t.Fatalf("turn off: %d, on=%v", rr.Code, on())
	}
	mustContain(t, e.get(t, e.r.path+"/settings", e.r.owner.token, http.StatusOK), "CI is off for this repository")
	e.req(http.MethodPost, e.r.path+"/settings/ci", e.r.owner.token, url.Values{"ci_enabled": {"on"}})
	if !on() {
		t.Error("CI did not turn back on")
	}
}
