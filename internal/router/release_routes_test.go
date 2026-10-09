package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (e metaEnv) seedRelease(t *testing.T, tag string, draft, prerelease bool) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO releases (repo_id, tag_name, name, body, is_draft, is_prerelease, author_id, published_at)
		VALUES ($1, $2, $3, '**body**', $4, $5, $6, CASE WHEN $4 THEN NULL ELSE NOW() END) RETURNING id`,
		e.repoID, tag, "name "+tag, draft, prerelease, e.owner.id).Scan(&id)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}
	return id
}

func (e metaEnv) releaseCol(t *testing.T, id int64, col string) string {
	t.Helper()
	var v string
	if err := e.db.QueryRow(`SELECT `+col+`::text FROM releases WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("release %s: %v", col, err)
	}
	return v
}

func TestReleases_CreateJSON(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.writer.token,
		json: `{"tag_name":"v1.0.0","name":"One","body":"notes"}`})
	wantStatus(t, rr, http.StatusCreated)
	var got struct {
		ID      int64  `json:"id"`
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.TagName != "v1.0.0" {
		t.Fatalf("response = %s (%v)", rr.Body.String(), err)
	}
	if e.releaseCol(t, got.ID, "is_draft") != "false" || e.releaseCol(t, got.ID, "published_at") == "" {
		t.Error("a non-draft release must be published immediately")
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token,
		json: `{"tag_name":"v1.1.0-rc1","is_draft":true,"is_prerelease":true}`})
	wantStatus(t, rr, http.StatusCreated)
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if e.releaseCol(t, got.ID, "is_draft") != "true" || e.releaseCol(t, got.ID, "is_prerelease") != "true" {
		t.Error("draft/prerelease flags were not stored")
	}
	var published *string
	if err := e.db.QueryRow(`SELECT published_at::text FROM releases WHERE id = $1`, got.ID).Scan(&published); err != nil || published != nil {
		t.Errorf("draft published_at = %v (%v), want NULL", published, err)
	}
}

func TestReleases_CreateHTMX(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, htmx: true,
		form: url.Values{"tag_name": {"v2.0.0"}, "name": {"Two"}, "body": {"b"}, "is_prerelease": {"true"}}})
	wantStatus(t, rr, http.StatusOK)
	if got := rr.Header().Get("HX-Redirect"); got != "/"+e.owner.name+"/"+e.repoName+"/releases" {
		t.Errorf("HX-Redirect = %q", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM releases WHERE repo_id = $1 AND tag_name = 'v2.0.0' AND is_prerelease`, e.repoID); n != 1 {
		t.Errorf("stored releases = %d, want 1", n)
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, htmx: true,
		form: url.Values{"tag_name": {"v2.0.0"}}})
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "already exists") {
		t.Errorf("HX-Trigger = %q, want an already-exists toast", rr.Header().Get("HX-Trigger"))
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, htmx: true,
		form: url.Values{"tag_name": {"v3.0.0"}, "target": {"no-such-branch"}}})
	wantStatus(t, rr, http.StatusInternalServerError)
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Could not create release") {
		t.Errorf("HX-Trigger = %q", rr.Header().Get("HX-Trigger"))
	}
}

func TestReleases_CreateRefusals(t *testing.T) {
	e := newGitMetaEnv(t)
	ok := `{"tag_name":"v9.9.9"}`
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/releases"), json: ok}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: e.path("/releases"), token: e.outsider.token, json: ok}, http.StatusForbidden},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/releases", token: e.owner.token, json: ok}, http.StatusNotFound},
		{"bad json", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"empty tag", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":""}`}, http.StatusUnprocessableEntity},
		{"tag with space", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"v 1"}`}, http.StatusUnprocessableEntity},
		{"tag escaping refs", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"../evil"}`}, http.StatusUnprocessableEntity},
		{"leading dot", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":".hidden"}`}, http.StatusUnprocessableEntity},
		{"trailing slash", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"v1/"}`}, http.StatusUnprocessableEntity},
		{"too long", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"` + strings.Repeat("a", 256) + `"}`}, http.StatusUnprocessableEntity},
		{"unknown target", metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"v8","target":"nope"}`}, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM releases WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("refused requests created %d releases", n)
	}

	e.archive(t)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: ok}), http.StatusForbidden)
	if n := e.count(t, `SELECT COUNT(*) FROM releases WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("archived repo got %d releases", n)
	}
}

func TestReleases_CreateReusesExistingTag(t *testing.T) {
	e := newGitMetaEnv(t)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"v1","is_draft":true}`}), http.StatusCreated)
	e.do(t, metaReq{method: "DELETE", target: e.path("/releases/%d", e.latestReleaseID(t)), token: e.owner.token})
	rr := e.do(t, metaReq{method: "POST", target: e.path("/releases"), token: e.owner.token, json: `{"tag_name":"v1"}`})
	wantStatus(t, rr, http.StatusCreated)
}

func (e metaEnv) latestReleaseID(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM releases WHERE repo_id = $1 ORDER BY id DESC LIMIT 1`, e.repoID).Scan(&id); err != nil {
		t.Fatalf("latest release id: %v", err)
	}
	return id
}

func TestReleases_ReadAPI(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "GET", target: e.path("/releases")})
	wantStatus(t, rr, http.StatusOK)
	if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
		t.Errorf("empty list = %q, want []", got)
	}
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/latest")}), http.StatusNotFound)

	old := e.seedRelease(t, "v1", false, false)
	e.seedRelease(t, "v2-draft", true, false)
	latest := e.seedRelease(t, "v3", false, false)

	rr = e.do(t, metaReq{method: "GET", target: e.path("/releases")})
	wantStatus(t, rr, http.StatusOK)
	var list []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || len(list) != 3 {
		t.Fatalf("list = %s (%v)", rr.Body.String(), err)
	}

	rr = e.do(t, metaReq{method: "GET", target: e.path("/releases/latest")})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, `"v3"`)

	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/%d", old)}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/x")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/999999999")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/releases"}), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/%d", latest)}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/latest"), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/latest"), token: e.owner.token}), http.StatusOK)
}

func TestReleases_Update(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedRelease(t, "v1", true, false)
	other := e.seedRelease(t, "v2", false, false)

	rr := e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.writer.token,
		json: `{"tag_name":"v1.1","name":"renamed","body":"new","is_prerelease":true}`})
	wantStatus(t, rr, http.StatusOK)
	if e.releaseCol(t, id, "tag_name") != "v1.1" || e.releaseCol(t, id, "name") != "renamed" || e.releaseCol(t, id, "is_prerelease") != "true" {
		t.Error("release was not updated")
	}
	if e.releaseCol(t, id, "published_at") == "" {
		t.Error("taking a draft out of draft state must stamp published_at")
	}

	rr = e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.owner.token, htmx: true,
		form: url.Values{"tag_name": {"v1.2"}, "name": {"n"}}})
	wantStatus(t, rr, http.StatusOK)
	if got := rr.Header().Get("HX-Redirect"); got != "/"+e.owner.name+"/"+e.repoName+"/releases/tag/v1.2" {
		t.Errorf("HX-Redirect = %q", got)
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "PATCH", target: e.path("/releases/%d", id), json: `{"tag_name":"x"}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.outsider.token, json: `{"tag_name":"x"}`}, http.StatusForbidden},
		{"bad id", metaReq{method: "PATCH", target: e.path("/releases/x"), token: e.owner.token, json: `{"tag_name":"x"}`}, http.StatusBadRequest},
		{"bad json", metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"invalid tag", metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.owner.token, json: `{"tag_name":"a b"}`}, http.StatusUnprocessableEntity},
		{"tag already used", metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.owner.token, json: `{"tag_name":"v2"}`}, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if e.releaseCol(t, id, "tag_name") != "v1.2" || e.releaseCol(t, other, "tag_name") != "v2" {
		t.Error("refused updates changed a tag")
	}

	e.archive(t)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d", id), token: e.owner.token, json: `{"tag_name":"v1.2","name":"archived edit"}`}), http.StatusOK)
}

func TestReleases_Delete(t *testing.T) {
	e := newGitMetaEnv(t)
	a, b := e.seedRelease(t, "v1", false, false), e.seedRelease(t, "v2", false, false)

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/releases/%d", a)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/releases/%d", a), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/releases/x"), token: e.owner.token}), http.StatusBadRequest)
	if n := e.count(t, `SELECT COUNT(*) FROM releases WHERE repo_id = $1`, e.repoID); n != 2 {
		t.Fatalf("refusals deleted releases: %d left", n)
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/releases/%d", a), token: e.writer.token}), http.StatusNoContent)
	rr := e.do(t, metaReq{method: "DELETE", target: e.path("/releases/%d", b), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if got := rr.Header().Get("HX-Redirect"); got != "/"+e.owner.name+"/"+e.repoName+"/releases" {
		t.Errorf("HX-Redirect = %q", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM releases WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("releases left = %d", n)
	}
}

func TestReleases_Publish(t *testing.T) {
	e := newGitMetaEnv(t)
	draft := e.seedRelease(t, "v1", true, false)
	live := e.seedRelease(t, "v2", false, false)

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d/publish", draft)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d/publish", draft), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/x/publish"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/999999999/publish"), token: e.owner.token}), http.StatusNotFound)
	if e.releaseCol(t, draft, "is_draft") != "true" {
		t.Fatal("refusals published the draft")
	}

	rr := e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d/publish", live), token: e.owner.token})
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	rr = e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d/publish", live), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "already published") {
		t.Errorf("HX-Trigger = %q", rr.Header().Get("HX-Trigger"))
	}

	rr = e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d/publish", draft), token: e.writer.token})
	wantStatus(t, rr, http.StatusOK)
	if e.releaseCol(t, draft, "is_draft") != "false" || e.releaseCol(t, draft, "published_at") == "" {
		t.Error("draft was not published")
	}

	again := e.seedRelease(t, "v3", true, false)
	rr = e.do(t, metaReq{method: "PATCH", target: e.path("/releases/%d/publish", again), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Error("HTMX publish must ask the page to refresh")
	}
}

func TestReleases_EditPrerelease(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedRelease(t, "v1", false, false)
	target := e.path("/releases/%d/prerelease", id)

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, form: url.Values{"is_prerelease": {"true"}}}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.outsider.token, form: url.Values{"is_prerelease": {"true"}}}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/x/prerelease"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/999999999/prerelease"), token: e.owner.token}), http.StatusNotFound)
	if e.releaseCol(t, id, "is_prerelease") != "false" {
		t.Fatal("refusals flagged the prerelease")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, form: url.Values{"is_prerelease": {"true"}}}), http.StatusNoContent)
	if e.releaseCol(t, id, "is_prerelease") != "true" {
		t.Error("prerelease flag not set")
	}
	rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"is_prerelease": {"false"}}})
	wantStatus(t, rr, http.StatusOK)
	if rr.Header().Get("HX-Refresh") != "true" || e.releaseCol(t, id, "is_prerelease") != "false" {
		t.Error("HTMX prerelease toggle must clear the flag and refresh")
	}
}

func TestReleases_TitleAndBodySections(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedRelease(t, "v1", false, false)
	title, body := e.path("/releases/%d/title", id), e.path("/releases/%d/body", id)

	rr := e.do(t, metaReq{method: "GET", target: title})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "name v1")
	if strings.Contains(rr.Body.String(), `name="name"`) {
		t.Error("anonymous viewers must not get the title edit form")
	}
	rr = e.do(t, metaReq{method: "GET", target: title + "?mode=edit", token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, `name="name"`)

	rr = e.do(t, metaReq{method: "GET", target: body})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>body</strong>")
	rr = e.do(t, metaReq{method: "GET", target: body + "?mode=edit", token: e.owner.token})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<textarea")

	for _, section := range []string{"title", "body"} {
		wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/x/%s", section)}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/releases/999999999/%s", section)}), http.StatusNotFound)
	}
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/releases/1/title"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/releases/1/body"}), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: title, token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: body, token: e.outsider.token}), http.StatusNotFound)
}

func TestReleases_EditTitleAndBody(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.seedRelease(t, "v1", false, false)
	title, body := e.path("/releases/%d/title", id), e.path("/releases/%d/body", id)

	for _, target := range []string{title, body} {
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, form: url.Values{"name": {"x"}, "body": {"x"}}}), http.StatusUnauthorized)
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.outsider.token, form: url.Values{"name": {"x"}, "body": {"x"}}}), http.StatusForbidden)
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/releases/999999999/title"), token: e.owner.token}), http.StatusNotFound)
	if e.releaseCol(t, id, "name") != "name v1" || e.releaseCol(t, id, "body") != "**body**" {
		t.Fatal("refusals edited the release")
	}

	rr := e.do(t, metaReq{method: "PATCH", target: title, token: e.writer.token, form: url.Values{"name": {"Fresh title"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Fresh title")
	if e.releaseCol(t, id, "name") != "Fresh title" {
		t.Error("title not stored")
	}

	rr = e.do(t, metaReq{method: "PATCH", target: body, token: e.owner.token, form: url.Values{"body": {"_italic_ text"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<em>italic</em>")
	if e.releaseCol(t, id, "body") != "_italic_ text" {
		t.Error("body not stored")
	}
}

func TestReleases_Pages(t *testing.T) {
	e := newGitMetaEnv(t)
	e.seedRelease(t, "v1.0", false, false)
	e.seedRelease(t, "v2.0-beta", false, true)
	e.seedRelease(t, "v3-draft", true, false)

	rr := e.page(t, e.pagePath("/releases"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "v1.0")
	bodyHas(t, rr, "v2.0-beta")

	rr = e.page(t, e.pagePath("/releases"), e.owner.token, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "/releases/new")

	rr = e.page(t, e.pagePath("/releases/tag/v1.0"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>body</strong>")
	bodyHas(t, rr, e.owner.name)
	wantStatus(t, e.page(t, e.pagePath("/releases/tag/v2.0-beta"), e.writer.token, false), http.StatusOK)
	wantStatus(t, e.page(t, e.pagePath("/releases/tag/nope"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/releases", "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/releases/tag/v1", "", false), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.page(t, e.pagePath("/releases"), e.outsider.token, false), http.StatusNotFound)
	wantStatus(t, e.page(t, e.pagePath("/releases/tag/v1.0"), e.outsider.token, false), http.StatusNotFound)
}

func TestReleases_NewPage(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.page(t, e.pagePath("/releases/new"), "", false)
	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Errorf("anonymous status = %d, want 401 or a redirect to login", rr.Code)
	}
	wantStatus(t, e.page(t, e.pagePath("/releases/new"), e.outsider.token, false), http.StatusForbidden)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/releases/new", e.owner.token, false), http.StatusNotFound)

	rr = e.page(t, e.pagePath("/releases/new"), e.writer.token, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "main")

	e.archive(t)
	wantStatus(t, e.page(t, e.pagePath("/releases/new"), e.owner.token, false), http.StatusForbidden)
}
