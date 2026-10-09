package router_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

var badRefNames = []string{"a..b", "x y", "bad.lock", "/lead", "trail/", "a//b", ".dot", "end.", "a@{b", "@", "t~", "c:d", "q?", "s*", "[b", `back\slash`, "ctl\x01"}

func TestBranchRoutes_CreateRefusesInvalidNames(t *testing.T) {
	e := newR2Env(t)
	for _, name := range badRefNames {
		rr := e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {name}}, false)
		wantStatus(t, rr, http.StatusUnprocessableEntity)
		if msg := r2DecodeJSON[map[string]string](t, rr)["error"]; !strings.Contains(msg, "invalid ref name") {
			t.Errorf("%q: error = %q, want it to say the name is invalid", name, msg)
		}
		if e.hasRef(t, "branch", name) {
			t.Errorf("branch %q was written despite the refusal", name)
		}
	}
	rr := e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {"-x"}}, false)
	wantStatus(t, rr, http.StatusUnprocessableEntity)

	for _, name := range []string{"feature/x", "v1.2.3", "fix_bug-2"} {
		wantStatus(t, e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {name}}, false), http.StatusCreated)
		if !e.hasRef(t, "branch", name) {
			t.Errorf("valid branch %q was not created", name)
		}
	}
}

func TestTagRoutes_CreateRefusesInvalidNames(t *testing.T) {
	e := newR2Env(t)
	for _, name := range badRefNames {
		rr := e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{"name": {name}}, false)
		wantStatus(t, rr, http.StatusUnprocessableEntity)
		if msg := r2DecodeJSON[map[string]string](t, rr)["error"]; !strings.Contains(msg, "invalid ref name") {
			t.Errorf("%q: error = %q, want it to say the name is invalid", name, msg)
		}
		if e.hasRef(t, "tag", name) {
			t.Errorf("tag %q was written despite the refusal", name)
		}
	}

	for _, name := range []string{"v1.2.3", "release/1.0", "fix_bug-2"} {
		wantStatus(t, e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{"name": {name}}, false), http.StatusCreated)
		if !e.hasRef(t, "tag", name) {
			t.Errorf("valid tag %q was not created", name)
		}
	}
}
