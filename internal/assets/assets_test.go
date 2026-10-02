package assets_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mkappworks-dev/cloudzilla-app/internal/assets"
)

var appJS = strings.Repeat("console.log('cloudzilla');\n", 100)

// The first 16 hex digits of sha256(appJS).
const appJSHash = "ad71a82d6e3c3fec"

func newSet(t *testing.T) *assets.Set {
	t.Helper()
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
