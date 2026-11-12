package router_test

// Integration tests for the account export through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newExportRouter(t *testing.T, runWorker bool) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg).WithStorage(backend)
	if runWorker {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Export.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
	}
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h, svc, db
}

func exportRow(t *testing.T, svc *service.Services, userID int64) *model.AccountExport {
	t.Helper()
	e, err := svc.Export.Latest(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAccountExport_DownloadNeedsThePasswordAndTheOwner(t *testing.T) {
	h, svc, db := newExportRouter(t, true)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, "o"+suffix, "password1")
	otherID, _ := testutil.SeedUserWithPassword(t, db, "x"+suffix, "password1")
	session := makeJWT(t, userID, "testpw_o"+suffix)
	otherSession := makeJWT(t, otherID, "testpw_x"+suffix)

	resp := serve(h, browserRequest(http.MethodPost, "/settings/export", session, url.Values{}))
	if resp.Code != http.StatusSeeOther || resp.Header().Get("Location") != "/settings#export" {
		t.Fatalf("request: %d to %q", resp.Code, resp.Header().Get("Location"))
	}
	takeAudit(t, db, model.AuditActionExportRequest, userID)
	var e *model.AccountExport
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if e = exportRow(t, svc, userID); e != nil && e.Status == model.ExportReady {
			break
		}
	}
	if e == nil || e.Status != model.ExportReady {
		t.Fatalf("export never became ready: %+v", e)
	}
	path := "/settings/export/" + strconv.FormatInt(e.ID, 10) + "/download"

	if page := serve(h, browserRequest(http.MethodGet, "/settings", session, nil)).Body.String(); !strings.Contains(page, "Download zip") || !strings.Contains(page, ">Ready<") {
		t.Errorf("the settings page does not offer the ready export")
	}
	for _, password := range []string{"", "wrong"} {
		resp := serve(h, browserRequest(http.MethodPost, path, session, withPassword(url.Values{}, password)))
		if resp.Code != http.StatusSeeOther || !strings.Contains(resp.Header().Get("Location"), "profile_error=reauth_failed") || strings.Contains(resp.Header().Get("Content-Type"), "zip") {
			t.Errorf("password %q: %d to %q; want a refusal", password, resp.Code, resp.Header().Get("Location"))
		}
	}
	resp = serve(h, browserRequest(http.MethodPost, path, otherSession, withPassword(url.Values{}, "password1")))
	if resp.Code != http.StatusSeeOther || !strings.Contains(resp.Header().Get("Location"), "export_gone") || resp.Body.Len() > 0 {
		t.Errorf("another user's download: %d to %q, %d bytes; want no file", resp.Code, resp.Header().Get("Location"), resp.Body.Len())
	}
	resp = serve(h, browserRequest(http.MethodPost, path, session, withPassword(url.Values{}, "password1")))
	if resp.Code != http.StatusOK || resp.Header().Get("Content-Type") != "application/zip" || resp.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("download: %d %q %q", resp.Code, resp.Header().Get("Content-Type"), resp.Header().Get("Cache-Control"))
	}
	if _, err := zip.NewReader(bytes.NewReader(resp.Body.Bytes()), int64(resp.Body.Len())); err != nil {
		t.Errorf("the body is not a zip: %v", err)
	}
	takeAudit(t, db, model.AuditActionExportDownload, userID)
}

func TestAccountExport_SettingsShowsEachState(t *testing.T) {
	h, _, db := newExportRouter(t, false)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)
	section := func() string {
		body := serve(h, browserRequest(http.MethodGet, "/settings", session, nil)).Body.String()
		start := strings.Index(body, `id="export"`)
		if start < 0 {
			t.Fatal("no export row")
		}
		return body[start : start+strings.Index(body[start:], "</li>")]
	}

	if got := section(); !strings.Contains(got, "Request export") || strings.Contains(got, "Coming soon") {
		t.Errorf("before a request:\n%s", got)
	}
	serve(h, browserRequest(http.MethodPost, "/settings/export", session, url.Values{}))
	if got := section(); !strings.Contains(got, ">Preparing<") || strings.Contains(got, "Request export") {
		t.Errorf("while queued:\n%s", got)
	}
	again := serve(h, browserRequest(http.MethodPost, "/settings/export", session, url.Values{}))
	if !strings.Contains(again.Header().Get("Location"), "export_in_progress") {
		t.Errorf("second request: %q", again.Header().Get("Location"))
	}
	testutil.Exec(t, db, `UPDATE account_exports SET status = 'failed', error = 'The export failed. Request a new one.' WHERE user_id = $1`, userID)
	if got := section(); !strings.Contains(got, ">Failed<") || !strings.Contains(got, "Request again") {
		t.Errorf("after a failure:\n%s", got)
	}
}
