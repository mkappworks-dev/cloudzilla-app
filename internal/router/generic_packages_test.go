package router_test

// Generic package upload and download through the router, the way curl and CI use them. Requires TEST_DATABASE_DSN
// and skips otherwise.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
)

type genericEnv struct {
	*registryEnv
	root string
}

func newGenericEnv(t *testing.T, maxFile, packageQuota int64) *genericEnv {
	t.Helper()
	return newGenericEnvWith(t, func(c *config.Config) {
		c.Packages.MaxFileBytes = maxFile
		c.Quota.User.PackageBytes = packageQuota
	})
}

func newGenericEnvWith(t *testing.T, mutate func(*config.Config)) *genericEnv {
	t.Helper()
	e := newRegistryEnv(t, mutate)
	root := t.TempDir()
	backend, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	e.svc.WithStorage(backend)
	return &genericEnv{registryEnv: e, root: root}
}

// objects is how many stored objects the backend holds.
func (e *genericEnv) objects(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(e.root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatalf("walk storage: %v", err)
	}
	return n
}

func (e *genericEnv) rows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (e *genericEnv) uploadURL(repo, name, version, filename string) string {
	return "/api/repos/" + e.owner + "/" + repo + "/packages/generic/" + name + "/" + version + "/" + filename
}

func (e *genericEnv) downloadURL(repo, name, version, filename string) string {
	return "/" + e.owner + "/" + repo + "/packages/generic/" + name + "/" + version + "/" + filename
}

// put is `curl -u user:token -T` when basic is true, otherwise a Bearer upload.
func (e *genericEnv) put(target, pat string, basic bool, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, target, bytes.NewReader(body))
	switch {
	case pat == "":
	case basic:
		req.SetBasicAuth("ci", pat)
	default:
		req.Header.Set("Authorization", "Bearer "+pat)
	}
	return serve(e.h, req)
}

func (e *genericEnv) get(target, pat string, basic bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	switch {
	case pat == "":
	case basic:
		req.SetBasicAuth("ci", pat)
	default:
		req.Header.Set("Authorization", "Bearer "+pat)
	}
	return serve(e.h, req)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestGenericUploadDownloadRoundTrip(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	body := bytes.Repeat([]byte("cloudzilla"), 1000)

	up := e.put(e.uploadURL("pub", "tool", "1.0.0", "tool.tar.gz"), e.ownerPAT, true, body)
	if up.Code != http.StatusCreated {
		t.Fatalf("basic upload = %d, want 201: %s", up.Code, up.Body)
	}
	var file struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	if err := json.Unmarshal(up.Body.Bytes(), &file); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if want := sha256Hex(body); file.SHA256 != want || file.Size != int64(len(body)) {
		t.Fatalf("upload response = %+v, want sha256 %s size %d", file, want, len(body))
	}

	// Public repository, so an anonymous download works.
	dl := e.get(e.downloadURL("pub", "tool", "1.0.0", "tool.tar.gz"), "", false)
	if dl.Code != http.StatusOK {
		t.Fatalf("anonymous download = %d, want 200", dl.Code)
	}
	got, _ := io.ReadAll(dl.Body)
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded %d bytes that differ from the %d uploaded", len(got), len(body))
	}
	if dl.Header().Get("ETag") != `"`+file.SHA256+`"` {
		t.Errorf("ETag = %q, want the stored sha256", dl.Header().Get("ETag"))
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"Cache-Control":           "private, no-cache",
		"Content-Disposition":     `attachment; filename="tool.tar.gz"`,
	} {
		if got := dl.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestGenericBearerUploadAndScope(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	target := e.uploadURL("pub", "tool", "1.0.0", "a.bin")

	if r := e.put(target, e.ownerPAT, false, []byte("bearer")); r.Code != http.StatusCreated {
		t.Fatalf("bearer upload = %d, want 201 (no CSRF 403): %s", r.Code, r.Body)
	}
	other := e.uploadURL("pub", "tool", "1.0.0", "b.bin")
	if r := e.put(other, e.ownerRepoWritePAT, false, []byte("x")); r.Code != http.StatusForbidden {
		t.Errorf("repo:write bearer upload = %d, want 403", r.Code)
	}
	if r := e.put(other, e.ownerRepoWritePAT, true, []byte("x")); r.Code != http.StatusForbidden {
		t.Errorf("repo:write basic upload = %d, want a plain 403, not 401", r.Code)
	}
	if r := e.put(other, e.ownerReadPAT, true, []byte("x")); r.Code != http.StatusForbidden {
		t.Errorf("packages:read basic upload = %d, want 403", r.Code)
	}
	if r := e.put(other, "czp_not-a-token", true, []byte("x")); r.Code != http.StatusUnauthorized {
		t.Errorf("upload with an unknown token = %d, want 401", r.Code)
	}
	if got := e.objects(t); got != 1 {
		t.Errorf("%d objects stored after refused uploads, want 1", got)
	}
}

func TestGenericNameValidation(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	for _, tc := range []struct{ name, version, filename string }{
		{"..", "1.0", "a.bin"},
		{".", "1.0", "a.bin"},
		{"tool", "..", "a.bin"},
		{"tool", ".", "a.bin"},
		{"tool", "1.0", ".."},
		{"tool", "1.0", "."},
		{"to%20ol", "1.0", "a.bin"},
		{"tool", "1.0", "a%2Fb.bin"},
		{"tool", "1.0", "a%00.bin"},
		{"to+ol", "1.0", "a.bin"},
	} {
		r := e.put(e.uploadURL("pub", tc.name, tc.version, tc.filename), e.ownerPAT, true, []byte("x"))
		if r.Code != http.StatusBadRequest {
			t.Errorf("upload %q/%q/%q = %d, want 400", tc.name, tc.version, tc.filename, r.Code)
		}
	}
	if n := e.objects(t); n != 0 {
		t.Errorf("%d objects stored for refused names", n)
	}
}

func TestGenericConflicts(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	if r := e.put(e.uploadURL("pub", "tool", "1.0.0", "a.bin"), e.ownerPAT, true, []byte("one")); r.Code != http.StatusCreated {
		t.Fatalf("first upload = %d: %s", r.Code, r.Body)
	}
	if r := e.put(e.uploadURL("pub", "tool", "1.0.0", "a.bin"), e.ownerPAT, true, []byte("two")); r.Code != http.StatusConflict {
		t.Errorf("re-upload = %d, want 409", r.Code)
	}
	if r := e.put(e.uploadURL("priv", "tool", "1.0.0", "a.bin"), e.ownerPAT, true, []byte("x")); r.Code != http.StatusConflict {
		t.Errorf("same package name under another repository = %d, want 409", r.Code)
	}
	dl := e.get(e.downloadURL("pub", "tool", "1.0.0", "a.bin"), "", false)
	if got := dl.Body.String(); got != "one" {
		t.Errorf("published file reads %q after a refused re-upload, want it unchanged", got)
	}
	if n, r := e.objects(t), e.rows(t, "package_files"); n != 1 || r != 1 {
		t.Errorf("%d objects and %d rows after refused uploads, want 1 and 1", n, r)
	}
}

func TestGenericPermissions(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)

	// reader holds packages:write but is only a reader of priv.
	if r := e.put(e.uploadURL("priv", "tool", "1.0.0", "a.bin"), e.readerPAT, true, []byte("x")); r.Code != http.StatusForbidden {
		t.Errorf("reader upload = %d, want 403", r.Code)
	}
	// A stranger can't tell a private repository from a missing one.
	if r := e.put(e.uploadURL("priv", "tool", "1.0.0", "a.bin"), e.strangerPAT, true, []byte("x")); r.Code != http.StatusNotFound {
		t.Errorf("stranger upload to private repo = %d, want 404", r.Code)
	}
	if r := e.put(e.uploadURL("priv", "tool", "1.0.0", "a.bin"), e.ownerPAT, true, []byte("private")); r.Code != http.StatusCreated {
		t.Fatalf("owner upload to priv = %d: %s", r.Code, r.Body)
	}
	target := e.downloadURL("priv", "tool", "1.0.0", "a.bin")
	if r := e.get(target, "", false); r.Code != http.StatusNotFound {
		t.Errorf("anonymous download of a private repo = %d, want 404", r.Code)
	}
	if r := e.get(target, e.strangerPAT, true); r.Code != http.StatusNotFound {
		t.Errorf("stranger download = %d, want 404", r.Code)
	}
	if r := e.get(target, e.readerPAT, true); r.Code != http.StatusOK {
		t.Errorf("reader basic download = %d, want 200", r.Code)
	}
	if r := e.get(target, e.ownerReadPAT, false); r.Code != http.StatusOK {
		t.Errorf("packages:read bearer download = %d, want 200", r.Code)
	}
	if r := e.get(target, e.ownerRepoWritePAT, true); r.Code != http.StatusForbidden {
		t.Errorf("repo:write basic download = %d, want 403", r.Code)
	}
	if r := e.get(e.downloadURL("priv", "tool", "1.0.0", "missing.bin"), e.ownerPAT, true); r.Code != http.StatusNotFound {
		t.Errorf("missing file = %d, want 404", r.Code)
	}

	// A repository made private stops serving at once.
	if r := e.put(e.uploadURL("pub", "tool2", "1.0.0", "a.bin"), e.ownerPAT, true, []byte("x")); r.Code != http.StatusCreated {
		t.Fatalf("upload to pub = %d: %s", r.Code, r.Body)
	}
	pubTarget := e.downloadURL("pub", "tool2", "1.0.0", "a.bin")
	if r := e.get(pubTarget, "", false); r.Code != http.StatusOK {
		t.Fatalf("anonymous download of public repo = %d, want 200", r.Code)
	}
	testExec(t, e, `UPDATE repositories SET private = TRUE WHERE name = 'pub' AND owner_id = $1`, e.ownerID)
	if r := e.get(pubTarget, "", false); r.Code != http.StatusNotFound {
		t.Errorf("download after the repo went private = %d, want 404", r.Code)
	}
}

func testExec(t *testing.T, e *genericEnv, query string, args ...any) {
	t.Helper()
	if _, err := e.db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func TestGenericLimitsLeaveNothingBehind(t *testing.T) {
	t.Run("max file bytes", func(t *testing.T) {
		e := newGenericEnv(t, 100, 0)
		r := e.put(e.uploadURL("pub", "tool", "1.0.0", "big.bin"), e.ownerPAT, true, bytes.Repeat([]byte("x"), 101))
		if r.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversize upload = %d, want 413", r.Code)
		}
		if n := e.objects(t); n != 0 {
			t.Errorf("%d objects after an oversize upload", n)
		}
		if n := e.rows(t, "packages WHERE type = 'generic'"); n != 0 {
			t.Errorf("%d package rows after an oversize upload", n)
		}
		if r := e.put(e.uploadURL("pub", "tool", "1.0.0", "ok.bin"), e.ownerPAT, true, bytes.Repeat([]byte("x"), 100)); r.Code != http.StatusCreated {
			t.Errorf("upload at the limit = %d, want 201: %s", r.Code, r.Body)
		}
	})

	t.Run("quota", func(t *testing.T) {
		e := newGenericEnv(t, 1<<20, 150)
		if r := e.put(e.uploadURL("pub", "tool", "1.0.0", "a.bin"), e.ownerPAT, true, bytes.Repeat([]byte("x"), 100)); r.Code != http.StatusCreated {
			t.Fatalf("first upload = %d: %s", r.Code, r.Body)
		}
		r := e.put(e.uploadURL("pub", "tool", "1.0.0", "b.bin"), e.ownerPAT, true, bytes.Repeat([]byte("x"), 60))
		if r.Code != http.StatusForbidden {
			t.Fatalf("upload over the quota = %d, want 403: %s", r.Code, r.Body)
		}
		if n, rows := e.objects(t), e.rows(t, "package_files"); n != 1 || rows != 1 {
			t.Errorf("%d objects and %d rows after a refused upload, want 1 and 1", n, rows)
		}
		if r := e.put(e.uploadURL("pub", "tool", "1.0.0", "c.bin"), e.ownerPAT, true, bytes.Repeat([]byte("x"), 50)); r.Code != http.StatusCreated {
			t.Errorf("upload reaching the quota exactly = %d, want 201: %s", r.Code, r.Body)
		}
	})
}

func TestGenericListDetailAndDelete(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	for _, f := range []string{"a.bin", "b.bin"} {
		if r := e.put(e.uploadURL("pub", "tool", "1.0.0", f), e.ownerPAT, true, []byte(f)); r.Code != http.StatusCreated {
			t.Fatalf("upload %s = %d: %s", f, r.Code, r.Body)
		}
	}
	if r := e.put(e.uploadURL("pub", "tool", "2.0.0", "a.bin"), e.ownerPAT, true, []byte("v2")); r.Code != http.StatusCreated {
		t.Fatalf("upload v2 = %d: %s", r.Code, r.Body)
	}
	base := "/api/repos/" + e.owner + "/pub/packages"

	list := e.get(base, "", false)
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", list.Code, list.Body)
	}
	var pkgs []struct {
		Name          string `json:"name"`
		Type          string `json:"type"`
		LatestVersion string `json:"latest_version"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &pkgs); err != nil || len(pkgs) != 1 || pkgs[0].Name != "tool" || pkgs[0].Type != model.PackageTypeGeneric {
		t.Fatalf("list = %s (%v), want one generic package named tool", list.Body, err)
	}

	detail := e.get(base+"/generic/tool", "", false)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail = %d: %s", detail.Code, detail.Body)
	}
	var d struct {
		Versions []struct {
			Version string `json:"version"`
			Files   []struct {
				Filename    string `json:"filename"`
				SHA256      string `json:"sha256"`
				DownloadURL string `json:"download_url"`
			} `json:"files"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil || len(d.Versions) != 2 {
		t.Fatalf("detail = %s (%v), want two versions", detail.Body, err)
	}
	for _, v := range d.Versions {
		if v.Version == "1.0.0" {
			if len(v.Files) != 2 || v.Files[0].SHA256 != sha256Hex([]byte(v.Files[0].Filename)) ||
				v.Files[0].DownloadURL != e.downloadURL("pub", "tool", "1.0.0", v.Files[0].Filename) {
				t.Errorf("version 1.0.0 files = %+v", v.Files)
			}
		}
	}

	// A reader can't delete; a writer can.
	if r := e.del(base+"/generic/tool/1.0.0/a.bin", e.readerPAT); r.Code != http.StatusForbidden && r.Code != http.StatusNotFound {
		t.Errorf("stranger-ish delete = %d, want 403 or 404", r.Code)
	}
	if r := e.del(base+"/generic/tool/1.0.0/a.bin", e.ownerPAT); r.Code != http.StatusNoContent {
		t.Fatalf("delete file = %d: %s", r.Code, r.Body)
	}
	if r := e.get(e.downloadURL("pub", "tool", "1.0.0", "a.bin"), "", false); r.Code != http.StatusNotFound {
		t.Errorf("download of a deleted file = %d, want 404", r.Code)
	}
	if n := e.objects(t); n != 2 {
		t.Errorf("%d objects after deleting a file, want 2", n)
	}
	if r := e.del(base+"/generic/tool/1.0.0", e.ownerPAT); r.Code != http.StatusNoContent {
		t.Fatalf("delete version = %d: %s", r.Code, r.Body)
	}
	if n := e.objects(t); n != 1 {
		t.Errorf("%d objects after deleting a version, want 1", n)
	}
	if r := e.del(base+"/generic/tool/1.0.0", e.ownerPAT); r.Code != http.StatusNotFound {
		t.Errorf("delete of a missing version = %d, want 404", r.Code)
	}
}

func (e *genericEnv) del(target, pat string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, target, nil)
	req.Header.Set("Authorization", "Bearer "+pat)
	return serve(e.h, req)
}

func TestGenericBasicPATRateLimitIsPerUser(t *testing.T) {
	e := newGenericEnvWith(t, func(c *config.Config) {
		c.Packages.MaxFileBytes = 1 << 20
		c.RateLimit = config.RateLimitConfig{Enabled: true, Window: time.Hour,
			Core: config.RateBudget{Authenticated: 2, Anonymous: 1000}}
	})
	target := e.downloadURL("pub", "tool", "1.0.0", "a.bin")
	from := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.SetBasicAuth("ci", e.ownerReadPAT)
		req.RemoteAddr = addr
		return serve(e.h, req)
	}
	from("192.0.2.1:1")
	from("192.0.2.2:1")
	if rr := from("192.0.2.3:1"); rr.Code != http.StatusTooManyRequests {
		t.Errorf("third request with one PAT from another address = %d, want 429: the bucket is per user, not per IP", rr.Code)
	}
}
