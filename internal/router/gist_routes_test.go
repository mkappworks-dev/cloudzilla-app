package router_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func createGist(t *testing.T, e transferEnv, a transferAccount, public bool, filename string) string {
	t.Helper()
	pub := "false"
	if public {
		pub = "true"
	}
	body := `{"description":"` + filename + `","public":` + pub + `,"files":[{"filename":"` + filename + `","content":"package main"}]}`
	rr := serve(e.h, importJSONRequest(http.MethodPost, "/api/gists/", a.session, body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create gist: got %d %s, want 201", rr.Code, rr.Body)
	}
	var g struct{ ID string }
	if err := json.Unmarshal(rr.Body.Bytes(), &g); err != nil || g.ID == "" {
		t.Fatalf("create gist body = %s (%v)", rr.Body, err)
	}
	return g.ID
}

func TestGists_APIRequireSignIn(t *testing.T) {
	e := newTransferEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/gists/"},
		{http.MethodGet, "/api/gists/file-row"},
		{http.MethodPatch, "/api/gists/abc"},
		{http.MethodDelete, "/api/gists/abc"},
	} {
		if rr := serve(e.h, browserRequest(tc.method, tc.path, "", nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
		}
	}
}

func TestGists_CreateValidation(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	tooBig := strings.Repeat("x", 1024*1024+1)
	for name, body := range map[string]string{
		"malformed json": `{`,
		"no files":       `{"public":true,"files":[]}`,
		"blank filename": `{"files":[{"filename":"  ","content":"x"}]}`,
		"duplicate name": `{"files":[{"filename":"a","content":"x"},{"filename":"a","content":"y"}]}`,
		"oversize file":  `{"files":[{"filename":"a","content":"` + tooBig + `"}]}`,
	} {
		if rr := serve(e.h, importJSONRequest(http.MethodPost, "/api/gists/", me.session, body)); rr.Code != http.StatusBadRequest {
			t.Errorf("create with %s: got %d, want 400", name, rr.Code)
		}
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM gists WHERE owner_id = $1`, me.id); n != 0 {
		t.Errorf("rejected requests stored %d gists", n)
	}
}

func TestGists_UpdateAndDeleteAreOwnerOnly(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	id := createGist(t, e, me, true, "a.go")
	path := "/api/gists/" + id
	update := `{"description":"changed","public":false,"files":[{"filename":"b.go","content":"new"}]}`

	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, other.session, update)); rr.Code != http.StatusForbidden {
		t.Errorf("another user's update: got %d, want 403", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodDelete, path, other.session, "")); rr.Code != http.StatusForbidden {
		t.Errorf("another user's delete: got %d, want 403", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, "/api/gists/nope", me.session, update)); rr.Code != http.StatusNotFound {
		t.Errorf("update of a missing gist: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodDelete, "/api/gists/nope", me.session, "")); rr.Code != http.StatusNotFound {
		t.Errorf("delete of a missing gist: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, me.session, `{`)); rr.Code != http.StatusBadRequest {
		t.Errorf("malformed update: got %d, want 400", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, me.session, `{"files":[]}`)); rr.Code != http.StatusBadRequest {
		t.Errorf("update without files: got %d, want 400", rr.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM gists WHERE id = $1 AND description = 'a.go' AND public`, id); n != 1 {
		t.Fatal("a refused request changed the gist")
	}

	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, me.session, update)); rr.Code != http.StatusNoContent {
		t.Fatalf("update: got %d %s, want 204", rr.Code, rr.Body)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM gists WHERE id = $1 AND description = 'changed' AND NOT public`, id); n != 1 {
		t.Error("update did not change the gist")
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM gist_files WHERE gist_id = $1 AND filename = 'b.go'`, id); n != 1 {
		t.Error("update did not replace the files")
	}

	if rr := serve(e.h, importJSONRequest(http.MethodDelete, path, me.session, "")); rr.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d, want 204", rr.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM gists WHERE id = $1`, id); n != 0 {
		t.Error("delete left the gist behind")
	}
}

func TestGists_PrivateGistIsHiddenFromOthers(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	private := createGist(t, e, me, false, "secret_"+testutil.UniqueSuffix(t)+".go")
	public := createGist(t, e, me, true, "open_"+testutil.UniqueSuffix(t)+".go")

	if rr := serve(e.h, browserRequest(http.MethodGet, "/gists/"+private, "", nil)); rr.Code != http.StatusNotFound {
		t.Errorf("anonymous view of a private gist: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/gists/"+private, other.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("another user's view of a private gist: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/gists/"+private, me.session, nil)); rr.Code != http.StatusOK {
		t.Errorf("owner's view of a private gist: got %d, want 200", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/gists/"+public, "", nil)); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "open_") {
		t.Errorf("anonymous view of a public gist: got %d, want 200 with its file", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodGet, "/gists/nope", "", nil)); rr.Code != http.StatusNotFound {
		t.Errorf("missing gist: got %d, want 404", rr.Code)
	}
}

func TestGists_Pages(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	createGist(t, e, me, false, "privfile_"+testutil.UniqueSuffix(t)+".go")
	public := createGist(t, e, me, true, "pubfile_"+testutil.UniqueSuffix(t)+".py")

	get := func(path, session string) *http.Response {
		return serve(e.h, browserRequest(http.MethodGet, path, session, nil)).Result()
	}
	if resp := get("/gists/new", ""); resp.StatusCode == http.StatusOK {
		t.Error("anonymous new-gist form rendered")
	}
	if resp := get("/gists/new", me.session); resp.StatusCode != http.StatusOK {
		t.Errorf("new-gist form: got %d, want 200", resp.StatusCode)
	}
	if resp := get("/gists/"+public+"/edit", ""); resp.StatusCode == http.StatusOK {
		t.Error("anonymous edit form rendered")
	}
	if resp := get("/gists/"+public+"/edit", other.session); resp.StatusCode != http.StatusForbidden {
		t.Errorf("another user's edit form: got %d, want 403", resp.StatusCode)
	}
	if resp := get("/gists/nope/edit", me.session); resp.StatusCode != http.StatusNotFound {
		t.Errorf("edit of a missing gist: got %d, want 404", resp.StatusCode)
	}
	if resp := get("/gists/"+public+"/edit", me.session); resp.StatusCode != http.StatusOK {
		t.Errorf("owner's edit form: got %d, want 200", resp.StatusCode)
	}

	list := serve(e.h, browserRequest(http.MethodGet, "/gists?tab=private&sort=name", me.session, nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "privfile_") || strings.Contains(list.Body.String(), "pubfile_") {
		t.Errorf("private tab: got %d, want only the private gist", list.Code)
	}
	list = serve(e.h, browserRequest(http.MethodGet, "/gists?tab=private&sort=created&page=1", other.session, nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "privfile_") {
		t.Errorf("another user's private tab: got %d, want none of my private gists", list.Code)
	}
	list = serve(e.h, browserRequest(http.MethodGet, "/gists?tab=private&page=x", "", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "privfile_") {
		t.Errorf("anonymous private tab: got %d, want the public list without private gists", list.Code)
	}
	list = serve(e.h, browserRequest(http.MethodGet, "/gists", "", nil))
	if list.Code != http.StatusOK {
		t.Errorf("public list: got %d, want 200", list.Code)
	}
}

func TestGists_FileRowFragment(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	for _, query := range []string{"?index=3", "?index=x", ""} {
		rr := serve(e.h, htmxRequest(browserRequest(http.MethodGet, "/api/gists/file-row"+query, me.session, nil)))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "filename") {
			t.Errorf("file-row%s: got %d %.200s, want a filename field", query, rr.Code, rr.Body)
		}
	}
}
