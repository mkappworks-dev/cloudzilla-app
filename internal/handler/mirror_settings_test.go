package handler_test

// Integration tests: the mirror API and its settings section. All tests
// require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func newMirrorAPIRouter(t *testing.T, db *sql.DB, reposRoot string) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Server:   config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:     config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:      config.GitConfig{ReposRoot: reposRoot},
		Security: config.SecurityConfig{SecretKey: strings.Repeat("k", 32)},
		Mirror: config.MirrorConfig{Enabled: true, MinInterval: 10 * time.Minute, DefaultInterval: 8 * time.Hour,
			MaxConcurrent: 1, Timeout: time.Minute},
	}
	h, err := router.New(service.New(store.New(db), cfg), cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h
}

type mirrorResponse struct {
	RemoteURL    string    `json:"remote_url"`
	AuthUsername string    `json:"auth_username"`
	HasToken     bool      `json:"has_token"`
	Interval     string    `json:"interval"`
	NextSyncAt   time.Time `json:"next_sync_at"`
	LastError    string    `json:"last_error"`
}

func decodeMirror(t *testing.T, rr *httptest.ResponseRecorder) mirrorResponse {
	t.Helper()
	var m mirrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return m
}

func TestMirrorAPI_ReadUpdateStop(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newMirrorAPIRouter(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	makeMirror(t, db, r.id)
	path := "/api/repos" + r.path + "/mirror"

	rr := requestAPI(api, http.MethodGet, path, r.owner.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	if m := decodeMirror(t, rr); m.RemoteURL != "https://example.com/upstream.git" || m.HasToken || m.Interval != "1h0m0s" {
		t.Errorf("GET = %+v", m)
	}

	start := time.Now()
	rr = requestAPIBody(api, http.MethodPatch, path, r.owner.token,
		`{"remote_url":"https://example.com/moved.git","auth_username":"bot","auth_token":"ghp_s3cret","interval":"2h"}`)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "ghp_s3cret") {
		t.Fatalf("PATCH: %d %s", rr.Code, rr.Body.String())
	}
	m := decodeMirror(t, rr)
	if m.RemoteURL != "https://example.com/moved.git" || m.AuthUsername != "bot" || !m.HasToken || m.Interval != "2h0m0s" {
		t.Errorf("PATCH = %+v", m)
	}
	if m.NextSyncAt.After(start.Add(time.Minute)) {
		t.Errorf("next sync %v after a new source, want it due now", m.NextSyncAt.Sub(start))
	}
	var sealed []byte
	_ = db.QueryRow(`SELECT auth_token_enc FROM repo_mirrors WHERE repo_id = $1`, r.id).Scan(&sealed)
	if len(sealed) == 0 || strings.Contains(string(sealed), "ghp_s3cret") {
		t.Errorf("stored token = %q, want it sealed", sealed)
	}

	rr = requestAPIBody(api, http.MethodPatch, path, r.owner.token, `{"interval":"1h"}`)
	if m := decodeMirror(t, rr); rr.Code != http.StatusOK || !m.HasToken || m.RemoteURL != "https://example.com/moved.git" {
		t.Errorf("interval-only PATCH changed other fields: %d %+v", rr.Code, m)
	}
	rr = requestAPIBody(api, http.MethodPatch, path, r.owner.token, `{"clear_token":true}`)
	if m := decodeMirror(t, rr); m.HasToken {
		t.Errorf("after clear_token: %+v", m)
	}

	for _, bad := range []struct{ body, want string }{
		{`{"interval":"1m"}`, "between 10 minutes and 30 days"},
		{`{"interval":"soon"}`, "interval"},
		{`{"remote_url":"git@example.com:a/b.git"}`, "URL"},
	} {
		rr := requestAPIBody(api, http.MethodPatch, path, r.owner.token, bad.body)
		if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), bad.want) {
			t.Errorf("PATCH %s: %d %s, want 422 mentioning %q", bad.body, rr.Code, rr.Body.String(), bad.want)
		}
	}

	if rr := requestAPI(api, http.MethodDelete, path, r.owner.token); rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rr.Code, rr.Body.String())
	}
	var left int
	_ = db.QueryRow(`SELECT COUNT(*) FROM repo_mirrors WHERE repo_id = $1`, r.id).Scan(&left)
	if left != 0 {
		t.Error("the mirror row survived Stop mirroring")
	}
	if rr := postForm(t, api, r.owner.token, "/api/repos"+r.path+"/branches", url.Values{"name": {"mine"}}); rr.Code != http.StatusCreated {
		t.Errorf("branch after Stop mirroring: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	if rr := requestAPI(api, http.MethodGet, path, r.owner.token); rr.Code != http.StatusNotFound {
		t.Errorf("GET after Stop mirroring: want 404, got %d", rr.Code)
	}

	// Audit entries are written in the background.
	var audits int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action IN ('repo.mirror.update', 'repo.mirror.delete')`, r.owner.id).Scan(&audits)
		if audits >= 4 {
			break
		}
	}
	if audits != 4 {
		t.Errorf("mirror audit entries = %d, want 3 updates and 1 delete", audits)
	}
	var meta string
	_ = db.QueryRow(`SELECT COALESCE(string_agg(metadata::text, ' '), '') FROM audit_log WHERE actor_id = $1`, r.owner.id).Scan(&meta)
	if strings.Contains(meta, "ghp_s3cret") {
		t.Error("the audit log holds the token")
	}
}

func TestMirrorAPI_NeedsManage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newMirrorAPIRouter(t, db, t.TempDir())
	repo := seedOwnedRepo(t, db, false)
	makeMirror(t, db, repo.id)
	writer := seedSignedInUser(t, db)
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`, writer.id, repo.id)
	path := "/api/repos" + repo.path + "/mirror"

	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		if rr := requestAPI(api, method, path, writer.token); rr.Code != http.StatusForbidden {
			t.Errorf("writer %s: want 403, got %d", method, rr.Code)
		}
	}
	if rr := requestAPI(api, http.MethodPost, path+"/sync", writer.token); rr.Code != http.StatusAccepted {
		t.Errorf("writer sync: want 202, got %d", rr.Code)
	}
}

func TestMirrorSettings_Section(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newMirrorAPIRouter(t, db, t.TempDir())
	repo := seedOwnedRepo(t, db, false)
	plain := seedOwnedRepo(t, db, false)
	makeMirror(t, db, repo.id)
	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+repo.path+"/mirror", repo.owner.token, `{"auth_username":"bot","auth_token":"ghp_s3cret"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("seed token: %d %s", rr.Code, rr.Body.String())
	}

	body := requestAPI(api, http.MethodGet, repo.path+"/settings", repo.owner.token).Body.String()
	for _, want := range []string{`id="mirror"`, "Pull mirror", `value="https://example.com/upstream.git"`, `value="bot"`,
		"Stored · enter a new one to replace it", "Remove the stored token", `<option value="1h0m0s" selected>1 hour</option>`,
		"Stop mirroring", `href="#mirror"`} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
	if strings.Contains(body, "ghp_s3cret") {
		t.Error("the settings page shows the token")
	}
	if strings.Contains(requestAPI(api, http.MethodGet, plain.path+"/settings", plain.owner.token).Body.String(), `id="mirror"`) {
		t.Error("a plain repo's settings show the mirror section")
	}
}

func TestMirrorSettings_FormSave(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newMirrorAPIRouter(t, db, t.TempDir())
	repo := seedOwnedRepo(t, db, false)
	makeMirror(t, db, repo.id)
	send := func(form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/repos"+repo.path+"/mirror", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+repo.owner.token)
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()
		api.ServeHTTP(rr, req)
		return rr
	}

	rr := send(url.Values{"remote_url": {"https://example.com/upstream.git"}, "auth_username": {""}, "auth_token": {""}, "interval": {"24h0m0s"}})
	if rr.Header().Get("HX-Redirect") != repo.path+"/settings" {
		t.Errorf("save: %d, HX-Redirect %q", rr.Code, rr.Header().Get("HX-Redirect"))
	}
	var secs int
	_ = db.QueryRow(`SELECT interval_seconds FROM repo_mirrors WHERE repo_id = $1`, repo.id).Scan(&secs)
	if secs != 86400 {
		t.Errorf("interval_seconds = %d, want 86400", secs)
	}

	rr = send(url.Values{"remote_url": {"ftp://example.com/x"}, "interval": {"24h0m0s"}})
	if rr.Header().Get("HX-Retarget") != "#mirror-form-error" {
		t.Errorf("bad URL: %d, HX-Retarget %q; want the form's error slot", rr.Code, rr.Header().Get("HX-Retarget"))
	}
}
