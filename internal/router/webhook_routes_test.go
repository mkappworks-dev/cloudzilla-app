package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const ownerPassword = "Owner-pass-123"

func (e metaEnv) givePassword(t *testing.T) {
	t.Helper()
	testutil.SetPassword(t, e.db, e.owner.id, ownerPassword)
}

// seedHook bypasses the create endpoint, whose URL guard refuses the unroutable address deliveries must fail against.
func (e metaEnv) seedHook(t *testing.T, events string) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO webhooks (repo_id, url, events) VALUES ($1, 'http://127.0.0.1:1/hook', $2) RETURNING id`, e.repoID, events).Scan(&id)
	if err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	return id
}

func (e metaEnv) seedDelivery(t *testing.T, hookID int64) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO webhook_deliveries (webhook_id, event, payload, response_code) VALUES ($1, 'push', '{}', 200) RETURNING id`, hookID).Scan(&id)
	if err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	return id
}

func (e metaEnv) hookEvents(t *testing.T, id int64) string {
	t.Helper()
	var ev string
	if err := e.db.QueryRow(`SELECT events FROM webhooks WHERE id = $1`, id).Scan(&ev); err != nil {
		t.Fatalf("hook events: %v", err)
	}
	return ev
}

func TestWebhooks_List(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "GET", target: e.path("/hooks"), token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
		t.Errorf("empty list = %q, want []", got)
	}

	id := e.seedHook(t, "push")
	rr = e.do(t, metaReq{method: "GET", target: e.path("/hooks"), token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	var hooks []struct {
		ID     int64  `json:"id"`
		Events string `json:"events"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &hooks); err != nil || len(hooks) != 1 || hooks[0].ID != id {
		t.Errorf("hooks = %s (%v)", rr.Body.String(), err)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/hooks")}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/hooks"), token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/hooks"), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/hooks", token: e.owner.token}), http.StatusNotFound)
}

func TestWebhooks_CreateJSON(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token,
		json: `{"url":"https://93.184.216.34/hook","secret":"s3","password":"` + ownerPassword + `"}`})
	wantStatus(t, rr, http.StatusCreated)
	var url, events string
	var active bool
	if err := e.db.QueryRow(`SELECT url, events, active FROM webhooks WHERE repo_id = $1`, e.repoID).Scan(&url, &events, &active); err != nil {
		t.Fatal(err)
	}
	if url != "https://93.184.216.34/hook" || events != "push,issues,pull_request" || !active {
		t.Errorf("stored hook = %q %q %v", url, events, active)
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token,
		json: `{"url":"http://93.184.216.34:8080/x","events":"push","password":"` + ownerPassword + `"}`})
	wantStatus(t, rr, http.StatusCreated)
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1 AND events = 'push'`, e.repoID); n != 1 {
		t.Errorf("custom events not stored: %d", n)
	}
}

func TestWebhooks_CreateRefusals(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)
	pw := `,"password":"` + ownerPassword + `"`
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/hooks"), json: `{"url":"https://93.184.216.34/h"` + pw + `}`}, http.StatusUnauthorized},
		{"writer", metaReq{method: "POST", target: e.path("/hooks"), token: e.writer.token, json: `{"url":"https://93.184.216.34/h"` + pw + `}`}, http.StatusForbidden},
		{"outsider", metaReq{method: "POST", target: e.path("/hooks"), token: e.outsider.token, json: `{"url":"https://93.184.216.34/h"` + pw + `}`}, http.StatusForbidden},
		{"bad json", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"missing url", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":""` + pw + `}`}, http.StatusBadRequest},
		{"no password", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"https://93.184.216.34/h"}`}, http.StatusForbidden},
		{"ftp scheme", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"ftp://93.184.216.34/h"` + pw + `}`}, http.StatusBadRequest},
		{"no host", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"http:///path"` + pw + `}`}, http.StatusBadRequest},
		{"loopback", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"http://127.0.0.1:9/h"` + pw + `}`}, http.StatusBadRequest},
		{"private range", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"http://10.0.0.5/h"` + pw + `}`}, http.StatusBadRequest},
		{"cloud metadata", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"http://169.254.169.254/latest"` + pw + `}`}, http.StatusBadRequest},
		{"ipv6 loopback", metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, json: `{"url":"http://[::1]/h"` + pw + `}`}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("refused requests created %d webhooks", n)
	}
}

func TestWebhooks_CreateHTMX(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, htmx: true,
		form: url.Values{"url": {"https://93.184.216.34/hook"}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "93.184.216.34")
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, e.repoID); n != 1 {
		t.Fatalf("webhooks = %d, want 1", n)
	}

	formErrors := []struct {
		name string
		form url.Values
		want string
	}{
		{"missing url", url.Values{"password": {ownerPassword}}, "Payload URL is required."},
		{"loopback", url.Values{"url": {"http://127.0.0.1/h"}, "password": {ownerPassword}}, "webhook.allow_local_networks"},
		{"bad scheme", url.Values{"url": {"gopher://93.184.216.34"}, "password": {ownerPassword}}, "http or https"},
		{"wrong password", url.Values{"url": {"https://93.184.216.34/h"}, "password": {"nope"}}, ""},
	}
	for _, c := range formErrors {
		t.Run(c.name, func(t *testing.T) {
			rr := e.do(t, metaReq{method: "POST", target: e.path("/hooks"), token: e.owner.token, htmx: true, form: c.form})
			if rr.Header().Get("HX-Retarget") != "#webhook-form-error" {
				t.Errorf("HX-Retarget = %q, want the form error slot (status %d)", rr.Header().Get("HX-Retarget"), rr.Code)
			}
			bodyHas(t, rr, c.want)
		})
	}
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, e.repoID); n != 1 {
		t.Errorf("form errors created webhooks: %d", n)
	}
}

func TestWebhooks_Delete(t *testing.T) {
	e := newGitMetaEnv(t)
	a, b := e.seedHook(t, "push"), e.seedHook(t, "push")

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/hooks/%d", a)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/hooks/%d", a), token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/hooks/x"), token: e.owner.token}), http.StatusBadRequest)
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, e.repoID); n != 2 {
		t.Fatalf("refusals deleted hooks: %d left", n)
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/hooks/%d", a), token: e.owner.token}), http.StatusNoContent)
	rr := e.do(t, metaReq{method: "DELETE", target: e.path("/hooks/%d", b), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("hooks left = %d", n)
	}
}

func TestWebhooks_DeleteOtherReposHookLeavesItAlone(t *testing.T) {
	e := newGitMetaEnv(t)
	other := newGitMetaEnv(t)
	foreign := other.seedHook(t, "push")

	e.do(t, metaReq{method: "DELETE", target: e.path("/hooks/%d", foreign), token: e.owner.token})
	if n := e.count(t, `SELECT COUNT(*) FROM webhooks WHERE id = $1`, foreign); n != 1 {
		t.Error("hook of another repo was deleted through this repo's URL")
	}
}

func TestWebhooks_Update(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedHook(t, "push,issues,pull_request")
	target := e.path("/hooks/%d", id)

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, json: `{"events":"push"}`}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"events":"push"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/hooks/x"), token: e.owner.token, json: `{"events":"push"}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"events":"push,deploy"}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"events":""}`}), http.StatusBadRequest)
	if got := e.hookEvents(t, id); got != "push,issues,pull_request" {
		t.Fatalf("refusals changed events to %q", got)
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"events":"push, issues"}`}), http.StatusNoContent)
	if got := e.hookEvents(t, id); got != "push, issues" {
		t.Errorf("events = %q", got)
	}

	rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"pull_request": {"1"}, "push": {"1"}}})
	wantStatus(t, rr, http.StatusOK)
	if got := e.hookEvents(t, id); got != "push,pull_request" {
		t.Errorf("HTMX events = %q, want push,pull_request", got)
	}

	rr = e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"unrelated": {"1"}}})
	wantStatus(t, rr, http.StatusBadRequest)
	if got := e.hookEvents(t, id); got != "push,pull_request" {
		t.Errorf("an empty selection changed events to %q", got)
	}
}

func TestWebhooks_Deliveries(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedHook(t, "push")
	e.seedDelivery(t, id)
	target := e.path("/hooks/%d/deliveries", id)

	rr := e.do(t, metaReq{method: "GET", target: target, token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	var ds []struct {
		Event        string `json:"event"`
		ResponseCode int    `json:"response_code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &ds); err != nil || len(ds) != 1 || ds[0].Event != "push" {
		t.Errorf("deliveries = %s (%v)", rr.Body.String(), err)
	}

	rr = e.do(t, metaReq{method: "GET", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "push")

	empty := e.seedHook(t, "push")
	rr = e.do(t, metaReq{method: "GET", target: e.path("/hooks/%d/deliveries", empty), token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
		t.Errorf("no deliveries = %q, want []", got)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: target, token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/hooks/x/deliveries"), token: e.owner.token}), http.StatusBadRequest)
	rr = e.do(t, metaReq{method: "GET", target: e.path("/hooks/999999999/deliveries"), token: e.owner.token})
	wantStatus(t, rr, http.StatusNotFound)
	bodyHas(t, rr, "webhook not found")

	other := newGitMetaEnv(t)
	foreign := other.seedHook(t, "push")
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/hooks/%d/deliveries", foreign), token: e.owner.token}), http.StatusForbidden)
}

func TestWebhooks_Redeliver(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedHook(t, "push")
	delivery := e.seedDelivery(t, id)
	target := e.path("/hooks/%d/redeliver?delivery_id=%d", id, delivery)

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/hooks/%d/redeliver", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/hooks/%d/redeliver?delivery_id=x", id), token: e.owner.token}), http.StatusBadRequest)
	rr := e.do(t, metaReq{method: "POST", target: e.path("/hooks/%d/redeliver?delivery_id=999999999", id), token: e.owner.token})
	wantStatus(t, rr, http.StatusNotFound)
	bodyHas(t, rr, "delivery not found")

	other := newGitMetaEnv(t)
	foreign := other.seedDelivery(t, other.seedHook(t, "push"))
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/hooks/%d/redeliver?delivery_id=%d", id, foreign), token: e.owner.token}), http.StatusForbidden)

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token}), http.StatusAccepted)
}
