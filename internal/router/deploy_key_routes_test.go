package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func (e metaEnv) deployKeyCount(t *testing.T) int {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM deploy_keys WHERE repo_id = $1`, e.repoID)
}

func (e metaEnv) addDeployKey(t *testing.T, title string) (id int64, key string) {
	t.Helper()
	key = sshPublicKey(t)
	rr := e.do(t, metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token,
		form: url.Values{"title": {title}, "public_key": {key}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusCreated)
	var k struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &k); err != nil || k.ID == 0 {
		t.Fatalf("add deploy key = %s (%v)", rr.Body.String(), err)
	}
	return k.ID, key
}

func TestDeployKeys_List(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)

	rr := e.do(t, metaReq{method: "GET", target: e.path("/keys"), token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)

	id, _ := e.addDeployKey(t, "ci key")
	rr = e.do(t, metaReq{method: "GET", target: e.path("/keys"), token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	var keys []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		ReadOnly bool   `json:"read_only"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &keys); err != nil || len(keys) != 1 || keys[0].ID != id || keys[0].Title != "ci key" || keys[0].ReadOnly {
		t.Errorf("keys = %s (%v)", rr.Body.String(), err)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/keys")}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/keys"), token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/keys"), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/keys", token: e.owner.token}), http.StatusNotFound)
}

func TestDeployKeys_Add(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)
	key := sshPublicKey(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token,
		form: url.Values{"title": {"read only"}, "public_key": {key}, "read_only": {"true"}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusCreated)
	if n := e.count(t, `SELECT COUNT(*) FROM deploy_keys WHERE repo_id = $1 AND read_only AND title = 'read only'`, e.repoID); n != 1 {
		t.Error("read_only=true did not store a read-only key")
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, htmx: true,
		form: url.Values{"title": {"deploy"}, "public_key": {sshPublicKey(t)}, "password": {ownerPassword}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "deploy")
	if n := e.count(t, `SELECT COUNT(*) FROM deploy_keys WHERE repo_id = $1 AND NOT read_only`, e.repoID); n != 1 {
		t.Error("an unchecked read_only must store a read-write key")
	}
}

func TestDeployKeys_AddRefusals(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)
	other := newGitMetaEnv(t)
	other.givePassword(t)
	_, takenKey := other.addDeployKey(t, "theirs")

	ok := url.Values{"title": {"k"}, "public_key": {sshPublicKey(t)}, "password": {ownerPassword}}
	with := func(k, v string) url.Values {
		out := url.Values{}
		for key, vals := range ok {
			out[key] = vals
		}
		out.Set(k, v)
		return out
	}
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/keys"), form: ok}, http.StatusUnauthorized},
		{"writer", metaReq{method: "POST", target: e.path("/keys"), token: e.writer.token, form: ok}, http.StatusForbidden},
		{"outsider", metaReq{method: "POST", target: e.path("/keys"), token: e.outsider.token, form: ok}, http.StatusForbidden},
		{"no title", metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, form: with("title", "")}, http.StatusBadRequest},
		{"no key", metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, form: with("public_key", "")}, http.StatusBadRequest},
		{"wrong password", metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, form: with("password", "wrong")}, http.StatusForbidden},
		{"garbage key", metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, form: with("public_key", "not a key")}, http.StatusUnprocessableEntity},
		{"key already a deploy key elsewhere", metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, form: with("public_key", takenKey)}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if e.deployKeyCount(t) != 0 {
		t.Errorf("refused requests added %d keys", e.deployKeyCount(t))
	}

	rr := e.do(t, metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, form: with("public_key", "not a key")})
	bodyHas(t, rr, "Invalid public key")
}

func TestDeployKeys_AddRejectsUserKey(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)
	userKey := sshPublicKey(t)
	if _, err := e.svc.SSHKey.AddKey(context.Background(), e.owner.id, "personal", userKey); err != nil {
		t.Fatalf("add user key: %v", err)
	}

	rr := e.do(t, metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, htmx: true,
		form: url.Values{"title": {"dup"}, "public_key": {userKey}, "password": {ownerPassword}}})
	if rr.Header().Get("HX-Retarget") != "#deploy-key-form-error" {
		t.Fatalf("HX-Retarget = %q (status %d)", rr.Header().Get("HX-Retarget"), rr.Code)
	}
	bodyHas(t, rr, "distinct from user SSH keys")
	if e.deployKeyCount(t) != 0 {
		t.Error("a user's SSH key became a deploy key")
	}

	for _, c := range []struct {
		name string
		form url.Values
		want string
	}{
		{"missing fields", url.Values{"title": {""}}, "Title and public key are required."},
		{"garbage key", url.Values{"title": {"t"}, "public_key": {"nope"}, "password": {ownerPassword}}, "Invalid public key"},
		{"wrong password", url.Values{"title": {"t"}, "public_key": {sshPublicKey(t)}, "password": {"wrong"}}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			rr := e.do(t, metaReq{method: "POST", target: e.path("/keys"), token: e.owner.token, htmx: true, form: c.form})
			if rr.Header().Get("HX-Retarget") != "#deploy-key-form-error" {
				t.Errorf("HX-Retarget = %q (status %d)", rr.Header().Get("HX-Retarget"), rr.Code)
			}
			bodyHas(t, rr, c.want)
		})
	}
}

func TestDeployKeys_Delete(t *testing.T) {
	e := newGitMetaEnv(t)
	e.givePassword(t)
	a, _ := e.addDeployKey(t, "a")
	b, _ := e.addDeployKey(t, "b")
	other := newGitMetaEnv(t)
	other.givePassword(t)
	foreign, _ := other.addDeployKey(t, "theirs")

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/keys/%d", a)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/keys/%d", a), token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/keys/x"), token: e.owner.token}), http.StatusBadRequest)
	if e.deployKeyCount(t) != 2 {
		t.Fatalf("refusals deleted keys: %d left", e.deployKeyCount(t))
	}

	e.do(t, metaReq{method: "DELETE", target: e.path("/keys/%d", foreign), token: e.owner.token})
	if other.deployKeyCount(t) != 1 {
		t.Error("a key of another repo was deleted through this repo's URL")
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/keys/%d", a), token: e.owner.token}), http.StatusNoContent)
	rr := e.do(t, metaReq{method: "DELETE", target: e.path("/keys/%d", b), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if e.deployKeyCount(t) != 0 {
		t.Errorf("keys left = %d", e.deployKeyCount(t))
	}
}
