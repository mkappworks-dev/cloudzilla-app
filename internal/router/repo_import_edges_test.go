package router_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestImportRoutes_RequireSignIn(t *testing.T) {
	h, _, _ := newImportRouter(t, false)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/imports"},
		{http.MethodGet, "/api/imports/abc"},
	} {
		req := importJSONRequest(tc.method, tc.path, "", "{}")
		if rr := serve(h, req); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
		}
	}
}

func TestStartImport_RefusalsByCause(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, _, jwt := importUser(t, db)

	for _, tc := range []struct {
		name string
		body string
		want int
		msg  string
	}{
		{"not json", `nope`, http.StatusBadRequest, "invalid request body"},
		{"invalid repo name", `{"clone_url":"https://example.com/a.git","name":"bad name!"}`, http.StatusUnprocessableEntity, ""},
		{"someone else's namespace", `{"clone_url":"https://example.com/a.git","name":"x","owner":"someone-else-entirely"}`, http.StatusForbidden, "only into your account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := serve(h, importJSONRequest(http.MethodPost, "/api/imports", jwt, tc.body))
			if rr.Code != tc.want {
				t.Errorf("got %d %s, want %d", rr.Code, rr.Body, tc.want)
			}
			if !strings.Contains(rr.Body.String(), tc.msg) {
				t.Errorf("body %s lacks %q", rr.Body, tc.msg)
			}
		})
	}
}

func TestImportStatusPage_UnknownJobIs404(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, _, jwt := importUser(t, db)
	if rr := serve(h, browserRequest(http.MethodGet, "/repos/import/no-such-job", jwt, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("unknown job page: got %d, want 404", rr.Code)
	}
	if rr := serve(h, importJSONRequest(http.MethodGet, "/api/imports/no-such-job", jwt, "")); rr.Code != http.StatusNotFound {
		t.Errorf("unknown job API: got %d, want 404", rr.Code)
	}
}

func TestImportPage_OwnerParamHonoredOnlyForOwnedOrgs(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, uname, jwt := importUser(t, db)

	rr := serve(h, browserRequest(http.MethodGet, "/repos/import?owner=not-my-org", jwt, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), uname) || strings.Contains(rr.Body.String(), `value="not-my-org"`) {
		t.Errorf("owner param for an org the viewer doesn't own: got %d, want the page defaulting to %s", rr.Code, uname)
	}
}
