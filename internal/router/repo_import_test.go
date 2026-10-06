package router_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newImportRouter(t *testing.T, allowLocal bool) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	return newImportRouterWith(t, allowLocal, config.MirrorConfig{})
}

func newImportRouterWith(t *testing.T, allowLocal bool, mirror config.MirrorConfig) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	// With no account left, every route redirects to /setup.
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		Import: config.ImportConfig{AllowLocalNetworks: allowLocal, Timeout: time.Minute},
		Mirror: mirror,
	}
	svc := service.New(store.New(db), cfg)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h, svc, db
}

func importUser(t *testing.T, db *sql.DB) (int64, string, string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)
	name := "testuser_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1`, name) })
	return id, name, makeJWT(t, id, name)
}

func importJSONRequest(method, target, session, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: session})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRF})
	req.Header.Set("X-CSRF-Token", testCSRF)
	return req
}

type importJSON struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Error     string `json:"error"`
	StatusURL string `json:"status_url"`
}

func postImport(t *testing.T, h http.Handler, jwt, body string) importJSON {
	t.Helper()
	rr := serve(h, importJSONRequest(http.MethodPost, "/api/imports", jwt, body))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("POST /api/imports = %d: %s", rr.Code, rr.Body)
	}
	var job importJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return job
}

func waitRouterImport(t *testing.T, svc *service.Services, userID int64, id string) service.ImportJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		job, err := svc.Import.Get(userID, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if job.Finished() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("import %s still %s", id, job.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A loopback source is refused at connect time under the default config, so
// these jobs fail fast without touching the network.
const refusedImportBody = `{"clone_url":"http://127.0.0.1:1/x.git","name":"refused"}`

func TestStartImport_Accepted(t *testing.T) {
	h, svc, db := newImportRouter(t, false)
	uid, uname, jwt := importUser(t, db)

	job := postImport(t, h, jwt, refusedImportBody)
	if job.ID == "" || job.Status != "queued" || job.Owner != uname || job.StatusURL != "/repos/import/"+job.ID {
		t.Errorf("response = %+v", job)
	}
	done := waitRouterImport(t, svc, uid, job.ID)
	if done.Status != service.ImportFailed || !strings.HasPrefix(done.Error, "127.0.0.1 resolves to a private network address") {
		t.Errorf("job = %s %q", done.Status, done.Error)
	}
}

func TestStartImport_RejectsBadInput(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, _, jwt := importUser(t, db)
	for _, tc := range []struct {
		body, want string
	}{
		{`{"clone_url":"/srv/repos/a/b.git","name":"x"}`, service.ErrImportURL.Error()},
		{`{"clone_url":"https://example.com/a.git","auth_token":"t","name":"x"}`, service.ErrImportCredentials.Error()},
		{`{"clone_url":"https://example.com/a.git","name":"bad name"}`, ""},
	} {
		rr := serve(h, importJSONRequest(http.MethodPost, "/api/imports", jwt, tc.body))
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422: %s", tc.body, rr.Code, rr.Body)
			continue
		}
		if tc.want != "" && !strings.Contains(rr.Body.String(), tc.want) {
			t.Errorf("%s: body %s, want %q", tc.body, rr.Body, tc.want)
		}
	}
}

func TestGetImportJob_OnlyForItsUser(t *testing.T) {
	h, svc, db := newImportRouter(t, false)
	uid, _, jwt := importUser(t, db)
	_, _, otherJWT := importUser(t, db)
	job := postImport(t, h, jwt, refusedImportBody)
	waitRouterImport(t, svc, uid, job.ID)

	rr := serve(h, importJSONRequest(http.MethodGet, "/api/imports/"+job.ID, jwt, ""))
	var got importJSON
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &got) != nil || got.Status != "failed" || got.Error == "" {
		t.Errorf("owner GET = %d %s", rr.Code, rr.Body)
	}
	if rr := serve(h, importJSONRequest(http.MethodGet, "/api/imports/"+job.ID, otherJWT, "")); rr.Code != http.StatusNotFound {
		t.Errorf("other user GET = %d, want 404", rr.Code)
	}
}

func TestImportPage_RendersWithPrefill(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, _, jwt := importUser(t, db)

	rr := serve(h, browserRequest(http.MethodGet, "/repos/import?url=https://example.com/a.git&name=prefilled", jwt, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /repos/import = %d", rr.Code)
	}
	for _, want := range []string{`id="import-form"`, `value="https://example.com/a.git"`, `value="prefilled"`} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestImportStatusPage_FailedJob(t *testing.T) {
	h, svc, db := newImportRouter(t, false)
	uid, _, jwt := importUser(t, db)
	_, _, otherJWT := importUser(t, db)
	job := postImport(t, h, jwt, refusedImportBody)
	waitRouterImport(t, svc, uid, job.ID)

	rr := serve(h, browserRequest(http.MethodGet, "/repos/import/"+job.ID, jwt, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "127.0.0.1 resolves to a private network address") ||
		!strings.Contains(rr.Body.String(), "Try again") {
		t.Errorf("owner page = %d, want the failure and a retry link", rr.Code)
	}
	if rr := serve(h, browserRequest(http.MethodGet, "/repos/import/"+job.ID, otherJWT, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("other user = %d, want 404", rr.Code)
	}
}

func TestImportStatus_DoneRedirectsToTheRepo(t *testing.T) {
	h, svc, db := newImportRouter(t, true)
	uid, uname, jwt := importUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")
	job := postImport(t, h, jwt, fmt.Sprintf(`{"clone_url":%q,"name":"imported"}`, url))
	if done := waitRouterImport(t, svc, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("import %s: %s", done.Status, done.Error)
	}
	repoURL := "/" + uname + "/imported"

	frag := browserRequest(http.MethodGet, "/repos/import/"+job.ID, jwt, nil)
	frag.Header.Set("HX-Request", "true")
	rr := serve(h, frag)
	if rr.Code != http.StatusOK || rr.Header().Get("HX-Redirect") != repoURL || strings.Contains(rr.Body.String(), "<html") {
		t.Errorf("fragment = %d, HX-Redirect %q", rr.Code, rr.Header().Get("HX-Redirect"))
	}
	rr = serve(h, browserRequest(http.MethodGet, "/repos/import/"+job.ID, jwt, nil))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != repoURL {
		t.Errorf("page = %d, Location %q; want 303 to %s", rr.Code, rr.Header().Get("Location"), repoURL)
	}
}

var routerMirrorsOn = config.MirrorConfig{Enabled: true, AllowLocalNetworks: true, MinInterval: 10 * time.Minute,
	DefaultInterval: 8 * time.Hour, MaxConcurrent: 1, Timeout: time.Minute}

func TestStartImport_Mirror(t *testing.T) {
	h, svc, db := newImportRouterWith(t, true, routerMirrorsOn)
	uid, uname, jwt := importUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")

	job := postImport(t, h, jwt, fmt.Sprintf(`{"clone_url":%q,"name":"mirrored","mirror":true,"mirror_interval":"1h"}`, url))
	if done := waitRouterImport(t, svc, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("import %s: %s", done.Status, done.Error)
	}
	repo, err := svc.Repo.Get(t.Context(), uname, "mirrored")
	if err != nil || !repo.IsMirror {
		t.Fatalf("repo = %+v, %v; want a mirror", repo, err)
	}
	var interval int
	if err := db.QueryRow(`SELECT interval_seconds FROM repo_mirrors WHERE repo_id = $1`, repo.ID).Scan(&interval); err != nil || interval != 3600 {
		t.Errorf("interval_seconds = %d, %v; want 3600", interval, err)
	}
	var audited int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = 'repo.mirror.create'`, uid).Scan(&audited)
	if audited != 1 {
		t.Errorf("repo.mirror.create audit entries = %d, want 1", audited)
	}
}

func TestStartImport_MirrorRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mirror config.MirrorConfig
		body   string
		want   string
	}{
		{"mirrors off", config.MirrorConfig{}, `{"clone_url":"https://example.com/a.git","name":"x","mirror":true}`, service.ErrMirrorsDisabled.Error()},
		{"too short", routerMirrorsOn, `{"clone_url":"https://example.com/a.git","name":"x","mirror":true,"mirror_interval":"5m"}`, "between 10 minutes and 30 days"},
		{"not a duration", routerMirrorsOn, `{"clone_url":"https://example.com/a.git","name":"x","mirror":true,"mirror_interval":"soon"}`, "mirror_interval"},
		{"token without a key", routerMirrorsOn, `{"clone_url":"https://example.com/a.git","name":"x","mirror":true,"auth_username":"a","auth_token":"t"}`, "security.secret_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, db := newImportRouterWith(t, false, tc.mirror)
			_, _, jwt := importUser(t, db)
			rr := serve(h, importJSONRequest(http.MethodPost, "/api/imports", jwt, tc.body))
			if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), tc.want) {
				t.Errorf("status %d %s, want 422 mentioning %q", rr.Code, rr.Body, tc.want)
			}
		})
	}
}

func TestImportPage_MirrorOption(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mirror config.MirrorConfig
		shown  bool
	}{{"mirrors on", routerMirrorsOn, true}, {"mirrors off", config.MirrorConfig{}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, db := newImportRouterWith(t, false, tc.mirror)
			_, _, jwt := importUser(t, db)
			rr := serve(h, browserRequest(http.MethodGet, "/repos/import", jwt, nil))
			body := rr.Body.String()
			if got := strings.Contains(body, "Keep this repository in sync"); got != tc.shown {
				t.Errorf("sync option shown = %v, want %v", got, tc.shown)
			}
			if tc.shown && !strings.Contains(body, `<option value="8h0m0s" selected>8 hours</option>`) {
				t.Error("the 8h default isn't the selected interval")
			}
		})
	}
}
