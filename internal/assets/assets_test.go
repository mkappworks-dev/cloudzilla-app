package assets_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mkappworks-dev/cloudzilla-app/internal/assets"
)

var appJS = strings.Repeat("console.log('cloudzilla');\n", 100)

// The first 16 hex digits of sha256(appJS).
const appJSHash = "ad71a82d6e3c3fec"

// newSet builds the hashing Set the release build serves from. Under the dev
// tag New answers with a dev Set instead, so those cases have nothing to say.
func newSet(t *testing.T) *assets.Set {
	t.Helper()
	if newIsDev {
		t.Skip("New returns a dev Set under the dev build tag")
	}
	set, err := assets.New(fstest.MapFS{"static/app.js": {Data: []byte(appJS)}})
	if err != nil {
		t.Fatalf("assets.New: %v", err)
	}
	return set
}

func get(set *assets.Set, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	set.ServeHTTP(rr, req)
	return rr
}

func TestURL(t *testing.T) {
	set := newSet(t)
	for path, want := range map[string]string{
		"/static/app.js":     "/static/app.js?v=" + appJSHash,
		"/static/missing.js": "/static/missing.js",
	} {
		if got := set.URL(path); got != want {
			t.Errorf("URL(%q) = %q, want %q", path, got, want)
		}
	}
}

// During an upgrade an old instance can still be asked for the new hash, so
// only the hash of the bytes being served earns a year-long cache.
func TestServeHTTP_CachesOnlyTheCurrentVersionForever(t *testing.T) {
	set := newSet(t)
	for target, want := range map[string]string{
		"/static/app.js?v=" + appJSHash:     "public, max-age=31536000, immutable",
		"/static/app.js":                    "no-cache",
		"/static/app.js?v=0123456789abcdef": "no-cache",
	} {
		rr := get(set, target, nil)
		if rr.Code != http.StatusOK || rr.Body.String() != appJS {
			t.Errorf("GET %s: status %d with %d body bytes, want 200 and the file", target, rr.Code, rr.Body.Len())
		}
		if got := rr.Header().Get("Cache-Control"); got != want {
			t.Errorf("GET %s: Cache-Control %q, want %q", target, got, want)
		}
	}
}

func TestServeHTTP_RevalidationGets304(t *testing.T) {
	set := newSet(t)
	etag := get(set, "/static/app.js", nil).Header().Get("ETag")
	if want := `"` + appJSHash + `"`; etag != want {
		t.Fatalf("ETag = %q, want %q", etag, want)
	}
	rr := get(set, "/static/app.js", map[string]string{"If-None-Match": etag})
	if rr.Code != http.StatusNotModified || rr.Body.Len() != 0 {
		t.Errorf("conditional GET: status %d with %d body bytes, want 304 and no body", rr.Code, rr.Body.Len())
	}
}

func TestServeHTTP_GzipsWhenAccepted(t *testing.T) {
	set := newSet(t)
	rr := get(set, "/static/app.js", map[string]string{"Accept-Encoding": "gzip, deflate, br, zstd"})
	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got, want := rr.Header().Get("ETag"), `"`+appJSHash+`-gzip"`; got != want {
		t.Errorf("ETag = %q, want %q: the gzip body needs its own validator", got, want)
	}
	if got := rr.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}
	if got, want := rr.Header().Get("Content-Length"), strconv.Itoa(rr.Body.Len()); got != want {
		t.Errorf("Content-Length = %q, want %s", got, want)
	}
	zr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	if body, err := io.ReadAll(zr); err != nil || string(body) != appJS {
		t.Errorf("decompressed body differs from the file (read error: %v)", err)
	}
}

func TestServeHTTP_UncompressedUnlessGzipAccepted(t *testing.T) {
	set := newSet(t)
	for _, accept := range []string{"", "br, zstd", "gzip;q=0, br", "deflate, GZIP ; Q=0.000"} {
		rr := get(set, "/static/app.js", map[string]string{"Accept-Encoding": accept})
		if got := rr.Header().Get("Content-Encoding"); got != "" || rr.Body.String() != appJS {
			t.Errorf("Accept-Encoding %q: got Content-Encoding %q, want the uncompressed file", accept, got)
		}
		if got := rr.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Errorf("Accept-Encoding %q: Vary = %q, want Accept-Encoding", accept, got)
		}
	}
}

// A range of a gzip response must index the gzip bytes, not the original file.
func TestServeHTTP_RangeOfGzipBody(t *testing.T) {
	set := newSet(t)
	full := get(set, "/static/app.js", map[string]string{"Accept-Encoding": "gzip"}).Body.Bytes()
	rr := get(set, "/static/app.js", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-9"})
	if rr.Code != http.StatusPartialContent || rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("status %d, Content-Encoding %q; want 206 and gzip", rr.Code, rr.Header().Get("Content-Encoding"))
	}
	if got, want := rr.Header().Get("Content-Range"), "bytes 0-9/"+strconv.Itoa(len(full)); got != want {
		t.Errorf("Content-Range = %q, want %q", got, want)
	}
	if len(full) < 10 || !bytes.Equal(rr.Body.Bytes(), full[:10]) {
		t.Errorf("range body isn't the first 10 bytes of the gzip body")
	}
}

func TestServeHTTP_ServesOnlyFiles(t *testing.T) {
	set := newSet(t)
	for _, target := range []string{"/static/missing.js", "/static/", "/static"} {
		if rr := get(set, target, nil); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rr.Code)
		}
	}
}

// A dev Set reads the working tree, so every answer has to come from the file
// as it is now. A real temp dir, not fstest.MapFS, because these cases turn on
// writes that land after the Set exists.

func newDevSet(t *testing.T) (*assets.Set, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, dir, "static/app.js", appJS)
	return assets.NewDev(os.DirFS(dir)), dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// The dev build tag is the only thing that puts a dev Set into a binary, and
// the cases above reach NewDev directly, so this is what covers the wiring.
func TestNew_FollowsTheBuildTag(t *testing.T) {
	dir := t.TempDir()
	set, err := assets.New(os.DirFS(dir))
	if err != nil {
		t.Fatalf("assets.New: %v", err)
	}
	writeFile(t, dir, "late.css", "body{color:red}")

	served := get(set, "/late.css", nil).Code == http.StatusOK
	if served != newIsDev {
		t.Errorf("New serves a file created after the walk = %v, want %v", served, newIsDev)
	}
}

func TestNewDev_URLFollowsTheFileOnDisk(t *testing.T) {
	set, dir := newDevSet(t)
	before := set.URL("/static/app.js")
	if want := "/static/app.js?v=" + appJSHash; before != want {
		t.Fatalf("URL = %q, want %q", before, want)
	}
	writeFile(t, dir, "static/app.js", appJS+"console.log('edited');\n")
	if after := set.URL("/static/app.js"); after == before {
		t.Errorf("URL = %q after an edit, want a different hash", after)
	}
}

func TestNewDev_URLOfAMissingFileIsUnchanged(t *testing.T) {
	set, _ := newDevSet(t)
	if got := set.URL("/static/missing.js"); got != "/static/missing.js" {
		t.Errorf("URL = %q, want the path unchanged", got)
	}
}

func TestNewDev_ServesAFileCreatedAfterTheSet(t *testing.T) {
	set, dir := newDevSet(t)
	writeFile(t, dir, "static/late.css", "body{color:red}")
	rr := get(set, "/static/late.css", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /static/late.css: status %d, want 200", rr.Code)
	}
	if got, want := rr.Body.String(), "body{color:red}"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// Both release behaviours would pin the previous edit for the rest of the
// session: the hashed URL is cached immutable, and the gzip body is memoized
// per file in a sync.Once.
func TestNewDev_NeverCachesAndNeverGzips(t *testing.T) {
	set, _ := newDevSet(t)
	rr := get(set, "/static/app.js", map[string]string{"Accept-Encoding": "gzip"})
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want none", got)
	}
	if rr.Body.String() != appJS {
		t.Errorf("body is not the file on disk")
	}
}

func TestNewDev_ServesOnlyFiles(t *testing.T) {
	set, _ := newDevSet(t)
	for _, target := range []string{"/static/missing.js", "/static/", "/static", "/../secret"} {
		if rr := get(set, target, nil); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rr.Code)
		}
	}
}
