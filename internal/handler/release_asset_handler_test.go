package handler_test

// Router-level tests for release assets. They require TEST_DATABASE_DSN.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	testAssetMaxBytes = 4 << 20
	testAssetMax      = 3
)

type releaseAssetEnv struct {
	avatarRouterEnv
}

func newReleaseAssetEnv(t *testing.T) releaseAssetEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:    config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:     config.GitConfig{ReposRoot: t.TempDir()},
		Release: config.ReleaseConfig{MaxAssetBytes: testAssetMaxBytes, MaxAssets: testAssetMax},
	}
	backend, err := storage.NewLocal(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(store.New(db), cfg).WithStorage(backend)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return releaseAssetEnv{avatarRouterEnv{db: db, svc: svc, router: h, backend: backend}}
}

func seedRelease(t *testing.T, db *sql.DB, repo seededRepo, tag string, draft bool) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO releases (repo_id, tag_name, name, is_draft, author_id) VALUES ($1, $2, '', $3, $4) RETURNING id`,
		repo.id, tag, draft, repo.owner.id).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e releaseAssetEnv) assetsPath(repo seededRepo, releaseID int64) string {
	return "/api/repos/" + repo.owner.name + "/" + repo.name + "/releases/" + strconv.FormatInt(releaseID, 10) + "/assets"
}

func (e releaseAssetEnv) upload(t *testing.T, path, token, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e releaseAssetEnv) do(t *testing.T, method, path, token string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

type uploadedReleaseAsset struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"download_url"`
}

func (e releaseAssetEnv) mustUpload(t *testing.T, repo seededRepo, releaseID int64, filename string, data []byte) uploadedReleaseAsset {
	t.Helper()
	rr := e.upload(t, e.assetsPath(repo, releaseID), repo.owner.token, filename, data)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload %s: %d %s", filename, rr.Code, rr.Body.String())
	}
	var a uploadedReleaseAsset
	if err := json.Unmarshal(rr.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestReleaseAsset_WriterUploadsAndAnyoneDownloadsFromAPublishedRelease(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1.0.0", false)
	data := []byte("<html><script>alert(1)</script></html>")

	a := e.mustUpload(t, repo, rel, "../evil page.html", data)
	sum := sha256.Sum256(data)
	if a.Name != "evil-page.html" || a.Size != int64(len(data)) || a.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("asset = %+v", a)
	}
	if want := repo.path + "/releases/download/v1.0.0/evil-page.html"; a.DownloadURL != want {
		t.Errorf("download_url = %q, want %q", a.DownloadURL, want)
	}

	got := e.do(t, http.MethodGet, a.DownloadURL, "", nil)
	if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), data) {
		t.Fatalf("anonymous GET: %d, %d bytes", got.Code, got.Body.Len())
	}
	for header, want := range map[string]string{
		"Content-Type":            "application/octet-stream",
		"Content-Disposition":     `attachment; filename="evil-page.html"`,
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"Cache-Control":           "private, no-cache",
		"Vary":                    "Authorization, Cookie",
		"Content-Length":          strconv.Itoa(len(data)),
	} {
		if v := got.Header().Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	if again := e.do(t, http.MethodGet, a.DownloadURL, "", map[string]string{"If-None-Match": got.Header().Get("ETag")}); again.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d, want 304", again.Code)
	}
	if head := e.do(t, http.MethodHead, a.DownloadURL, "", nil); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD: %d with %d body bytes", head.Code, head.Body.Len())
	}
}

func TestReleaseAsset_UploadIsNotCappedAtTheJSONLimit(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", false)
	a := e.mustUpload(t, repo, rel, "big.bin", bytes.Repeat([]byte("x"), 3<<20))
	if a.Size != 3<<20 {
		t.Errorf("size = %d", a.Size)
	}
}

func TestReleaseAsset_UploadAndDeleteNeedWriteAccess(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	stranger := seedSignedInUser(t, e.db)
	rel := seedRelease(t, e.db, repo, "v1", false)
	a := e.mustUpload(t, repo, rel, "a.txt", []byte("a"))
	path := e.assetsPath(repo, rel)

	if rr := e.upload(t, path, "", "b.txt", []byte("b")); rr.Code != http.StatusForbidden && rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous upload: %d, want 401 or 403", rr.Code)
	}
	if rr := e.upload(t, path, stranger.token, "b.txt", []byte("b")); rr.Code != http.StatusForbidden {
		t.Errorf("non-writer upload: %d, want 403", rr.Code)
	}
	del := path + "/" + strconv.FormatInt(a.ID, 10)
	if rr := e.do(t, http.MethodDelete, del, stranger.token, nil); rr.Code != http.StatusForbidden {
		t.Errorf("non-writer delete: %d, want 403", rr.Code)
	}
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", nil); rr.Code != http.StatusOK {
		t.Errorf("asset gone after refused delete: %d", rr.Code)
	}
}

func TestReleaseAsset_UploadWithoutCSRFHeaderIsRefusedUnread(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", false)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "a.txt")
	_, _ = part.Write([]byte("a"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, e.assetsPath(repo, rel), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: repo.owner.token})
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("cookie session without X-CSRF-Token: %d, want 403", rr.Code)
	}
}

func TestReleaseAsset_Limits(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", true)
	path := e.assetsPath(repo, rel)
	e.mustUpload(t, repo, rel, "one.bin", []byte("1"))

	for _, tc := range []struct {
		name     string
		filename string
		data     []byte
		status   int
		message  string
	}{
		{"duplicate", "one.bin", []byte("again"), http.StatusConflict, "already has an asset named one.bin"},
		{"duplicate after cleaning", "one bin", []byte("x"), http.StatusCreated, ""},
		{"over size", "huge.bin", bytes.Repeat([]byte("x"), testAssetMaxBytes+1), http.StatusRequestEntityTooLarge, "at most 4 MiB"},
		{"nameless", "...", []byte("x"), http.StatusUnprocessableEntity, "Choose a file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.upload(t, path, repo.owner.token, tc.filename, tc.data)
			if rr.Code != tc.status || !strings.Contains(rr.Body.String(), tc.message) {
				t.Errorf("%d %s, want %d containing %q", rr.Code, rr.Body.String(), tc.status, tc.message)
			}
		})
	}
	e.mustUpload(t, repo, rel, "three.bin", []byte("3"))
	if rr := e.upload(t, path, repo.owner.token, "four.bin", []byte("4")); rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "at most 3 assets") {
		t.Errorf("over count: %d %s", rr.Code, rr.Body.String())
	}

	var objects int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM release_assets WHERE release_id = $1`, rel).Scan(&objects)
	if objects != testAssetMax {
		t.Errorf("rows = %d, want %d", objects, testAssetMax)
	}
}

func TestReleaseAsset_RejectsAMissingFile(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", false)
	req := httptest.NewRequest(http.MethodPost, e.assetsPath(repo, rel), strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+repo.owner.token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("non-multipart body: %d, want 422", rr.Code)
	}
}

func TestReleaseAsset_DraftAssetsAreForWritersOnly(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	stranger := seedSignedInUser(t, e.db)
	rel := seedRelease(t, e.db, repo, "v2", true)
	a := e.mustUpload(t, repo, rel, "draft.bin", []byte("secret"))

	if rr := e.do(t, http.MethodGet, a.DownloadURL, repo.owner.token, nil); rr.Code != http.StatusOK {
		t.Errorf("writer: %d, want 200", rr.Code)
	}
	unknown := e.do(t, http.MethodGet, repo.path+"/releases/download/v2/nope.bin", "", nil)
	for who, token := range map[string]string{"anonymous": "", "signed-in non-writer": stranger.token} {
		rr := e.do(t, http.MethodGet, a.DownloadURL, token, nil)
		if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
			t.Errorf("%s: %d %q, want the unknown-asset 404 %q", who, rr.Code, rr.Body.String(), unknown.Body.String())
		}
	}
	// A matching ETag must not turn into a 304 for someone who can't read it.
	etag := e.do(t, http.MethodGet, a.DownloadURL, repo.owner.token, nil).Header().Get("ETag")
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", map[string]string{"If-None-Match": etag}); rr.Code != http.StatusNotFound {
		t.Errorf("anonymous with the ETag: %d, want 404", rr.Code)
	}

	list := func(token string) []uploadedReleaseAsset {
		rr := e.do(t, http.MethodGet, "/api/repos/"+repo.owner.name+"/"+repo.name+"/releases/"+strconv.FormatInt(rel, 10), token, nil)
		var body struct {
			Assets []uploadedReleaseAsset `json:"assets"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || rr.Code != http.StatusOK {
			t.Fatalf("GET release: %d %s", rr.Code, rr.Body.String())
		}
		return body.Assets
	}
	if got := list(repo.owner.token); len(got) != 1 || got[0].Name != "draft.bin" {
		t.Errorf("writer's assets = %+v", got)
	}
	if got := list(stranger.token); len(got) != 0 {
		t.Errorf("non-writer sees a draft's assets: %+v", got)
	}

	// Publishing makes the same URL public.
	testutil.Exec(t, e.db, `UPDATE releases SET is_draft = false WHERE id = $1`, rel)
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", nil); rr.Code != http.StatusOK {
		t.Errorf("after publish, anonymous: %d, want 200", rr.Code)
	}
}

func TestReleaseAsset_PrivateRepoFollowsCanRead(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, true)
	stranger := seedSignedInUser(t, e.db)
	rel := seedRelease(t, e.db, repo, "v1", false)
	a := e.mustUpload(t, repo, rel, "app.bin", []byte("app"))

	if rr := e.do(t, http.MethodGet, a.DownloadURL, repo.owner.token, nil); rr.Code != http.StatusOK {
		t.Errorf("owner: %d, want 200", rr.Code)
	}
	unknown := e.do(t, http.MethodGet, repo.path+"/releases/download/v1/nope.bin", "", nil)
	for who, token := range map[string]string{"anonymous": "", "signed-in non-collaborator": stranger.token} {
		rr := e.do(t, http.MethodGet, a.DownloadURL, token, nil)
		if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
			t.Errorf("%s: %d %q, want the unknown-asset 404", who, rr.Code, rr.Body.String())
		}
	}
	// Permission is re-checked per request: making the repo private again stops serving at once.
	testutil.Exec(t, e.db, `UPDATE repositories SET private = false WHERE id = $1`, repo.id)
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", nil); rr.Code != http.StatusOK {
		t.Fatalf("public: %d", rr.Code)
	}
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, repo.id)
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", nil); rr.Code != http.StatusNotFound {
		t.Errorf("made private: %d, want 404", rr.Code)
	}
}

func TestReleaseAsset_TagWithSlashStaysInOneSegment(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "rel/v1.2", false)
	a := e.mustUpload(t, repo, rel, "app.bin", []byte("app"))
	if want := repo.path + "/releases/download/" + url.PathEscape("rel/v1.2") + "/app.bin"; a.DownloadURL != want {
		t.Fatalf("download_url = %q, want %q", a.DownloadURL, want)
	}
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", nil); rr.Code != http.StatusOK {
		t.Errorf("GET: %d, want 200", rr.Code)
	}
}

func TestReleaseAsset_DeleteRemovesRowAndObject(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", false)
	a := e.mustUpload(t, repo, rel, "gone.bin", []byte("x"))
	var key string
	if err := e.db.QueryRow(`SELECT storage_key FROM release_assets WHERE id = $1`, a.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "releases/repo/"+strconv.FormatInt(repo.id, 10)+"/"+strconv.FormatInt(rel, 10)+"/") || !strings.HasSuffix(key, ".bin") || strings.Contains(key, "gone") {
		t.Errorf("storage key = %q", key)
	}

	del := e.assetsPath(repo, rel) + "/" + strconv.FormatInt(a.ID, 10)
	if rr := e.do(t, http.MethodDelete, del, repo.owner.token, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}
	if ok, err := e.backend.Exists(context.Background(), key); err != nil || ok {
		t.Errorf("object after delete: exists=%v err=%v", ok, err)
	}
	if rr := e.do(t, http.MethodGet, a.DownloadURL, "", nil); rr.Code != http.StatusNotFound {
		t.Errorf("GET after delete: %d, want 404", rr.Code)
	}
	if rr := e.do(t, http.MethodDelete, del, repo.owner.token, nil); rr.Code != http.StatusNotFound {
		t.Errorf("second delete: %d, want 404", rr.Code)
	}
}

func TestReleaseAsset_DeletingTheReleaseRemovesItsObjects(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", false)
	e.mustUpload(t, repo, rel, "a.bin", []byte("a"))
	var key string
	_ = e.db.QueryRow(`SELECT storage_key FROM release_assets WHERE release_id = $1`, rel).Scan(&key)

	rr := e.do(t, http.MethodDelete, "/api/repos/"+repo.owner.name+"/"+repo.name+"/releases/"+strconv.FormatInt(rel, 10), repo.owner.token, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete release: %d %s", rr.Code, rr.Body.String())
	}
	if ok, _ := e.backend.Exists(context.Background(), key); ok {
		t.Error("object survived its release")
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM release_assets WHERE release_id = $1`, rel).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows survived their release", n)
	}
}

func TestReleaseAsset_PurgingTheRepoRemovesItsObjects(t *testing.T) {
	e := newReleaseAssetEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	rel := seedRelease(t, e.db, repo, "v1", false)
	e.mustUpload(t, repo, rel, "a.bin", []byte("a"))
	var key string
	_ = e.db.QueryRow(`SELECT storage_key FROM release_assets WHERE release_id = $1`, rel).Scan(&key)

	testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, repo.id)
	if err := e.svc.Repo.PurgeExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.backend.Exists(context.Background(), key); ok {
		t.Error("object survived its repo's purge")
	}
}
