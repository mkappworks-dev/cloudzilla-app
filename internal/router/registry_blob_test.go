package router_test

// Blob uploads, pulls and mount over the /v2 wire protocol. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// req sends body to target with the given headers and returns the recorded response.
func (e *registryEnv) req(method, target, auth string, body io.Reader, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return serve(e.h, r)
}

// pushMonolithic opens a session and commits data in one PUT, the way a small layer goes.
func (e *registryEnv) pushMonolithic(t *testing.T, auth, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	start := e.req(http.MethodPost, "/v2/"+name+"/blobs/uploads/", auth, nil)
	if start.Code != http.StatusAccepted {
		t.Fatalf("start upload: got %d %s", start.Code, start.Body)
	}
	return e.req(http.MethodPut, start.Header().Get("Location")+"?digest="+digestOf(data), auth, bytes.NewReader(data))
}

// objectCount counts the files the storage backend holds.
func (e *registryEnv) objectCount(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(e.objects, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *registryEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBlobPush_MonolithicThenPull(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.bearer(t, e.ownerPAT, "repository:"+e.pubImage+":pull,push")
	data := payload(t, "layer one")

	put := e.pushMonolithic(t, auth, e.pubImage, data)

	if put.Code != http.StatusCreated {
		t.Fatalf("commit: got %d %s, want 201", put.Code, put.Body)
	}
	want := "/v2/" + e.pubImage + "/blobs/" + digestOf(data)
	if got := put.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if got := put.Header().Get("Docker-Content-Digest"); got != digestOf(data) {
		t.Errorf("Docker-Content-Digest = %q", got)
	}

	head := e.req(http.MethodHead, want, auth, nil)
	if head.Code != http.StatusOK || head.Header().Get("Content-Length") != strconv.Itoa(len(data)) {
		t.Errorf("HEAD: got %d length %q", head.Code, head.Header().Get("Content-Length"))
	}
	get := e.req(http.MethodGet, want, auth, nil)
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), data) {
		t.Errorf("GET: got %d %q", get.Code, get.Body)
	}
	if got := get.Header().Get("Docker-Content-Digest"); got != digestOf(data) {
		t.Errorf("GET Docker-Content-Digest = %q", got)
	}
	if cc := get.Header().Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Errorf("Cache-Control = %q, want private", cc)
	}
}

// payload is content no earlier test run put in the shared test database, whose blob rows outlive the temporary
// object store a run writes to.
func payload(t *testing.T, label string) []byte {
	t.Helper()
	return []byte(label + "/" + testutil.UniqueSuffix(t) + "/" + strconv.FormatInt(time.Now().UnixNano(), 36))
}

// forget drops any blob row left by an earlier run for fixed content.
func forget(t *testing.T, e *registryEnv, digest string) {
	t.Helper()
	testutil.Exec(t, e.db, `DELETE FROM package_blobs WHERE digest = $1`, digest)
	testutil.Exec(t, e.db, `DELETE FROM registry_blobs WHERE digest = $1`, digest)
}

// writer is a collaborator who may push to pub but has no access to priv.
func (e *registryEnv) writer(t *testing.T) (id int64, pat string) {
	t.Helper()
	id = testutil.SeedUser(t, e.db, "wrt_"+testutil.UniqueSuffix(t))
	name := userName(t, e.db, id)
	pub, err := e.svc.Repo.GetByID(context.Background(), e.pubRepoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Repo.AddCollaborator(context.Background(), pub, e.ownerID, name, string(model.RoleWriter)); err != nil {
		t.Fatal(err)
	}
	return id, e.pat(t, id, model.ScopePackagesWrite)
}

func (e *registryEnv) pushAuth(t *testing.T, pat string, names ...string) string {
	t.Helper()
	scopes := make([]string, len(names))
	for i, n := range names {
		scopes[i] = "repository:" + n + ":pull,push"
	}
	return e.bearer(t, pat, scopes...)
}

func (e *registryEnv) startUpload(t *testing.T, auth, name string) string {
	t.Helper()
	rr := e.req(http.MethodPost, "/v2/"+name+"/blobs/uploads/", auth, nil)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("start upload on %s: got %d %s", name, rr.Code, rr.Body)
	}
	return rr.Header().Get("Location")
}

func TestBlobPush_SameBytesToTwoPackagesStoreOneObject(t *testing.T) {
	e := newRegistryEnv(t, nil)
	pubRepo := e.pubRepoID
	testutil.Exec(t, e.db, `INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id) VALUES ($1, 'container', 'second', $2, $1)`, e.ownerID, pubRepo)
	second := e.owner + "/second"
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage, second)
	data := payload(t, "shared layer")

	for _, name := range []string{e.pubImage, second} {
		if rr := e.pushMonolithic(t, auth, name, data); rr.Code != http.StatusCreated {
			t.Fatalf("push to %s: got %d %s", name, rr.Code, rr.Body)
		}
	}

	if got := e.objectCount(t); got != 1 {
		t.Errorf("objects stored = %d, want 1", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM registry_blobs WHERE digest = $1`, digestOf(data)); got != 1 {
		t.Errorf("blob rows = %d, want 1", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM package_blobs WHERE digest = $1`, digestOf(data)); got != 2 {
		t.Errorf("link rows = %d, want 2", got)
	}
}

func TestBlobPush_ChunkedWithAndWithoutContentRange(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	data := payload(t, "0123456789abcdef")

	for _, withRange := range []bool{false, true} {
		loc := e.startUpload(t, auth, e.pubImage)
		first := e.req(http.MethodPatch, loc, auth, bytes.NewReader(data[:6]), rangeHeader(withRange, "0-5")...)
		if first.Code != http.StatusAccepted || first.Header().Get("Range") != "0-5" {
			t.Fatalf("first chunk (range sent %v): got %d Range %q", withRange, first.Code, first.Header().Get("Range"))
		}
		loc = first.Header().Get("Location")
		second := e.req(http.MethodPatch, loc, auth, bytes.NewReader(data[6:10]), rangeHeader(withRange, "6-9")...)
		if second.Code != http.StatusAccepted || second.Header().Get("Range") != "0-9" {
			t.Fatalf("second chunk: got %d Range %q", second.Code, second.Header().Get("Range"))
		}
		status := e.req(http.MethodGet, loc, auth, nil)
		if status.Code != http.StatusNoContent || status.Header().Get("Range") != "0-9" || status.Header().Get("Docker-Upload-UUID") == "" {
			t.Errorf("status: got %d Range %q UUID %q", status.Code, status.Header().Get("Range"), status.Header().Get("Docker-Upload-UUID"))
		}
		done := e.req(http.MethodPut, loc+"?digest="+digestOf(data), auth, bytes.NewReader(data[10:]))
		if done.Code != http.StatusCreated {
			t.Fatalf("commit: got %d %s", done.Code, done.Body)
		}
	}
	get := e.req(http.MethodGet, "/v2/"+e.pubImage+"/blobs/"+digestOf(data), auth, nil)
	if !bytes.Equal(get.Body.Bytes(), data) {
		t.Errorf("pulled %q, want %q", get.Body, data)
	}
	if got := e.objectCount(t); got != 1 {
		t.Errorf("objects stored = %d, want 1", got)
	}
}

func rangeHeader(send bool, v string) []string {
	if !send {
		return nil
	}
	return []string{"Content-Range", v}
}

func TestBlobPush_OutOfOrderChunkIs416WithTheHeldRange(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	loc := e.startUpload(t, auth, e.pubImage)

	empty := e.req(http.MethodPatch, loc, auth, strings.NewReader("zzzz"), "Content-Range", "5-8")
	if empty.Code != http.StatusRequestedRangeNotSatisfiable || empty.Header().Get("Range") != "0-0" {
		t.Errorf("empty session: got %d Range %q, want 416 0-0", empty.Code, empty.Header().Get("Range"))
	}
	ok := e.req(http.MethodPatch, loc, auth, strings.NewReader("abcde"), "Content-Range", "0-4")
	if ok.Code != http.StatusAccepted {
		t.Fatalf("in-order chunk: got %d %s", ok.Code, ok.Body)
	}
	again := e.req(http.MethodPatch, loc, auth, strings.NewReader("abcde"), "Content-Range", "0-4")
	if again.Code != http.StatusRequestedRangeNotSatisfiable || again.Header().Get("Range") != "0-4" {
		t.Errorf("replayed chunk: got %d Range %q, want 416 0-4", again.Code, again.Header().Get("Range"))
	}
}

func TestBlobPush_DigestMismatchLeavesNothingBehind(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	loc := e.startUpload(t, auth, e.pubImage)

	rr := e.req(http.MethodPut, loc+"?digest="+digestOf([]byte("other")), auth, strings.NewReader("actual"))

	if rr.Code != http.StatusBadRequest || ociErrorCode(t, rr) != "DIGEST_INVALID" {
		t.Fatalf("got %d %s, want 400 DIGEST_INVALID", rr.Code, rr.Body)
	}
	if got := e.objectCount(t); got != 0 {
		t.Errorf("objects stored = %d, want 0", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM registry_blobs WHERE digest = $1`, digestOf([]byte("actual"))); got != 0 {
		t.Errorf("blob rows = %d, want 0", got)
	}
	if got := e.req(http.MethodGet, loc, auth, nil); got.Code != http.StatusNotFound {
		t.Errorf("session after mismatch: got %d, want 404", got.Code)
	}
}

func TestBlobPush_OtherAlgorithmsAreDigestInvalid(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	loc := e.startUpload(t, auth, e.pubImage)

	rr := e.req(http.MethodPut, loc+"?digest=sha512:"+strings.Repeat("a", 128), auth, strings.NewReader("x"))

	if rr.Code != http.StatusBadRequest || ociErrorCode(t, rr) != "DIGEST_INVALID" {
		t.Errorf("got %d %s, want 400 DIGEST_INVALID", rr.Code, rr.Body)
	}
}

func TestBlobPush_WrongSizeIsSizeInvalid(t *testing.T) {
	e := newRegistryEnv(t, func(c *config.Config) { c.Registry.MaxBlobBytes = 8 })
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)

	loc := e.startUpload(t, auth, e.pubImage)
	mismatch := e.req(http.MethodPatch, loc, auth, strings.NewReader("abc"), "Content-Range", "0-9")
	if mismatch.Code != http.StatusBadRequest || ociErrorCode(t, mismatch) != "SIZE_INVALID" {
		t.Errorf("range/body mismatch: got %d %s, want 400 SIZE_INVALID", mismatch.Code, mismatch.Body)
	}
	tooBig := e.req(http.MethodPatch, loc, auth, strings.NewReader("123456789"))
	if tooBig.Code != http.StatusRequestEntityTooLarge || ociErrorCode(t, tooBig) != "SIZE_INVALID" {
		t.Errorf("over max_blob_bytes: got %d %s, want 413 SIZE_INVALID", tooBig.Code, tooBig.Body)
	}
	if st := e.req(http.MethodGet, loc, auth, nil); st.Header().Get("Range") != "0-0" {
		t.Errorf("a refused chunk left %q in the session, want none", st.Header().Get("Range"))
	}
}

func TestBlobPush_PendingPackageBelongsToItsCreator(t *testing.T) {
	e := newRegistryEnv(t, nil)
	wid, wpat := e.writer(t)
	oauth := e.pushAuth(t, e.ownerPAT, e.newImage)
	wauth := e.pushAuth(t, wpat, e.newImage)
	data := payload(t, "pending layer")

	loc := e.startUpload(t, oauth, e.newImage)
	if rr := e.req(http.MethodPut, loc+"?digest="+digestOf(data), oauth, bytes.NewReader(data)); rr.Code != http.StatusCreated {
		t.Fatalf("owner push: got %d %s", rr.Code, rr.Body)
	}

	blob := "/v2/" + e.newImage + "/blobs/" + digestOf(data)
	if rr := e.req(http.MethodHead, blob, oauth, nil); rr.Code != http.StatusOK {
		t.Errorf("creator HEAD of own pending blob: got %d, want 200", rr.Code)
	}
	if rr := e.req(http.MethodHead, blob, wauth, nil); rr.Code != http.StatusNotFound {
		t.Errorf("another writer HEAD: got %d, want 404", rr.Code)
	}
	if rr := e.req(http.MethodPatch, loc, wauth, strings.NewReader("x")); rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "BLOB_UPLOAD_UNKNOWN" {
		t.Errorf("another writer continuing the session: got %d %s", rr.Code, rr.Body)
	}

	other := e.startUpload(t, wauth, e.newImage)
	if other == loc {
		t.Error("two creators share a session")
	}
	if got := e.count(t, `SELECT COUNT(*) FROM packages WHERE name = 'newimg' AND repo_id IS NULL AND creator_id IN ($1, $2)`, e.ownerID, wid); got != 2 {
		t.Errorf("pending packages for the name = %d, want one per creator (2)", got)
	}
}

func TestBlobPush_TwentyFirstPendingPackageIsRefused(t *testing.T) {
	e := newRegistryEnv(t, nil)
	for i := range 20 {
		name := e.owner + "/pend" + strconv.Itoa(i)
		auth := e.bearer(t, e.ownerPAT, "repository:"+name+":pull,push,delete")
		loc := e.startUpload(t, auth, name)
		// Closed again so the per-user session cap doesn't hide the pending cap; the package stays pending.
		if rr := e.req(http.MethodDelete, loc, auth, nil); rr.Code != http.StatusNoContent {
			t.Fatalf("cancel: got %d %s", rr.Code, rr.Body)
		}
	}
	name := e.owner + "/pend20"

	rr := e.req(http.MethodPost, "/v2/"+name+"/blobs/uploads/", e.pushAuth(t, e.ownerPAT, name), nil)

	if rr.Code != http.StatusTooManyRequests || ociErrorCode(t, rr) != "TOOMANYREQUESTS" {
		t.Errorf("got %d %s, want 429 TOOMANYREQUESTS", rr.Code, rr.Body)
	}
}

func TestBlobPush_SessionExpiresAfterTheUploadTTL(t *testing.T) {
	e := newRegistryEnv(t, nil)
	loc := e.startUpload(t, e.pushAuth(t, e.ownerPAT, e.newImage), e.newImage)

	e.clock.now = e.clock.now.Add(24*time.Hour + time.Second)
	fresh := e.pushAuth(t, e.ownerPAT, e.newImage)

	rr := e.req(http.MethodPatch, loc, fresh, strings.NewReader("late"))
	if rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "BLOB_UPLOAD_UNKNOWN" {
		t.Errorf("got %d %s, want 404 BLOB_UPLOAD_UNKNOWN", rr.Code, rr.Body)
	}
}

func TestBlobPush_SessionIsBoundToItsPackage(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage, e.privImage)
	loc := e.startUpload(t, auth, e.pubImage)
	uuid := loc[strings.LastIndex(loc, "/")+1:]

	rr := e.req(http.MethodPatch, "/v2/"+e.privImage+"/blobs/uploads/"+uuid, auth, strings.NewReader("x"))

	if rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "BLOB_UPLOAD_UNKNOWN" {
		t.Errorf("got %d %s, want 404 BLOB_UPLOAD_UNKNOWN", rr.Code, rr.Body)
	}
}

func TestBlobPush_ElevenhSessionIsRefused(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	for range 10 {
		e.startUpload(t, auth, e.pubImage)
	}

	rr := e.req(http.MethodPost, "/v2/"+e.pubImage+"/blobs/uploads/", auth, nil)

	if rr.Code != http.StatusTooManyRequests || ociErrorCode(t, rr) != "TOOMANYREQUESTS" {
		t.Errorf("got %d %s, want 429 TOOMANYREQUESTS", rr.Code, rr.Body)
	}
}

func TestBlobPush_InflightByteCapIsRefused(t *testing.T) {
	e := newRegistryEnv(t, func(c *config.Config) { c.Registry.MaxInflightBytesPerUser = 10 })
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	a, b := e.startUpload(t, auth, e.pubImage), e.startUpload(t, auth, e.pubImage)

	if rr := e.req(http.MethodPatch, a, auth, strings.NewReader("123456")); rr.Code != http.StatusAccepted {
		t.Fatalf("first chunk: got %d %s", rr.Code, rr.Body)
	}
	rr := e.req(http.MethodPatch, b, auth, strings.NewReader("12345"))

	if rr.Code != http.StatusTooManyRequests || ociErrorCode(t, rr) != "TOOMANYREQUESTS" {
		t.Errorf("got %d %s, want 429 TOOMANYREQUESTS", rr.Code, rr.Body)
	}
}

func TestBlobPush_QuotaIsRefusedMidPatch(t *testing.T) {
	e := newRegistryEnv(t, func(c *config.Config) { c.Quota.User.PackageBytes = 10 })
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	loc := e.startUpload(t, auth, e.pubImage)

	if rr := e.req(http.MethodPatch, loc, auth, strings.NewReader("123456")); rr.Code != http.StatusAccepted {
		t.Fatalf("within quota: got %d %s", rr.Code, rr.Body)
	}
	rr := e.req(http.MethodPatch, loc, auth, strings.NewReader("12345"))

	if rr.Code != http.StatusForbidden || ociErrorCode(t, rr) != "DENIED" {
		t.Errorf("got %d %s, want 403 DENIED", rr.Code, rr.Body)
	}
}

func TestBlobPush_ReaderCannotUpload(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.bearer(t, e.readerPAT, "repository:"+e.privImage+":pull,push")

	rr := e.req(http.MethodPost, "/v2/"+e.privImage+"/blobs/uploads/", auth, nil)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d %s, want 401", rr.Code, rr.Body)
	}
}

func TestBlobPull_NeedsALinkAndReadAccessOnEveryRequest(t *testing.T) {
	e := newRegistryEnv(t, nil)
	oauth := e.pushAuth(t, e.ownerPAT, e.pubImage, e.privImage)
	data := payload(t, "private layer")
	if rr := e.pushMonolithic(t, oauth, e.privImage, data); rr.Code != http.StatusCreated {
		t.Fatalf("push: %d %s", rr.Code, rr.Body)
	}
	blob := func(name string) string { return "/v2/" + name + "/blobs/" + digestOf(data) }

	// Known digest, other package: the object exists but this package has no link.
	if rr := e.req(http.MethodGet, blob(e.pubImage), oauth, nil); rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "BLOB_UNKNOWN" {
		t.Errorf("unlinked blob: got %d %s, want 404 BLOB_UNKNOWN", rr.Code, rr.Body)
	}
	// A stranger asking for the private name is refused at the token, as for a name that does not exist.
	stranger := e.bearer(t, e.strangerPAT, "repository:"+e.privImage+":pull")
	if rr := e.req(http.MethodGet, blob(e.privImage), stranger, nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("stranger: got %d, want 401", rr.Code)
	}
	if rr := e.req(http.MethodGet, blob(e.privImage), e.bearer(t, e.readerPAT, "repository:"+e.privImage+":pull"), nil); rr.Code != http.StatusOK {
		t.Errorf("reader: got %d, want 200", rr.Code)
	}

	// A token granted while the Repository was readable stops working when it goes private.
	anon := e.bearer(t, "", "repository:"+e.pubImage+":pull")
	if rr := e.pushMonolithic(t, oauth, e.pubImage, data); rr.Code != http.StatusCreated {
		t.Fatalf("push to pub: %d %s", rr.Code, rr.Body)
	}
	if rr := e.req(http.MethodGet, blob(e.pubImage), anon, nil); rr.Code != http.StatusOK {
		t.Fatalf("anonymous pull of a public image: got %d", rr.Code)
	}
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.pubRepoID)
	rr := e.req(http.MethodGet, blob(e.pubImage), anon, nil)
	if rr.Code != http.StatusNotFound || ociErrorCode(t, rr) != "NAME_UNKNOWN" {
		t.Errorf("after going private: got %d %s, want 404 NAME_UNKNOWN", rr.Code, rr.Body)
	}
}

func TestBlobPull_RangeSkipsBytes(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	data := []byte("0123456789")
	forget(t, e, digestOf(data))
	e.pushMonolithic(t, auth, e.pubImage, data)

	rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/blobs/"+digestOf(data), auth, nil, "Range", "bytes=3-6")

	if rr.Code != http.StatusPartialContent || rr.Body.String() != "3456" {
		t.Errorf("got %d %q, want 206 3456", rr.Code, rr.Body)
	}
	if got := rr.Header().Get("Content-Range"); got != "bytes 3-6/10" {
		t.Errorf("Content-Range = %q", got)
	}
}

func TestBlobMount_CopiesALinkOnlyWhenTheSourceIsReadable(t *testing.T) {
	e := newRegistryEnv(t, nil)
	_, wpat := e.writer(t)
	oauth := e.pushAuth(t, e.ownerPAT, e.pubImage, e.privImage)
	data := payload(t, "mountable")
	e.pushMonolithic(t, oauth, e.privImage, data)
	mount := "/v2/" + e.pubImage + "/blobs/uploads/?mount=" + digestOf(data) + "&from=" + e.privImage

	// The writer cannot read priv, so the mount falls back to an ordinary upload.
	wauth := e.pushAuth(t, wpat, e.pubImage)
	rr := e.req(http.MethodPost, mount, wauth, nil)
	if rr.Code != http.StatusAccepted || rr.Header().Get("Location") == "" {
		t.Errorf("unreadable source: got %d, want 202 with a session", rr.Code)
	}
	if rr := e.req(http.MethodHead, "/v2/"+e.pubImage+"/blobs/"+digestOf(data), wauth, nil); rr.Code != http.StatusNotFound {
		t.Errorf("blob leaked through a refused mount: got %d", rr.Code)
	}

	// The owner reads priv, so the link is copied and no second object is stored.
	rr = e.req(http.MethodPost, mount, oauth, nil)
	if rr.Code != http.StatusCreated || rr.Header().Get("Location") != "/v2/"+e.pubImage+"/blobs/"+digestOf(data) {
		t.Errorf("readable source: got %d Location %q, want 201", rr.Code, rr.Header().Get("Location"))
	}
	if rr := e.req(http.MethodGet, "/v2/"+e.pubImage+"/blobs/"+digestOf(data), oauth, nil); rr.Code != http.StatusOK {
		t.Errorf("mounted blob: got %d", rr.Code)
	}
	if got := e.objectCount(t); got != 1 {
		t.Errorf("objects stored = %d, want 1", got)
	}
}

// boundedReader serves n zero bytes and records the largest buffer the reader of it offered.
type boundedReader struct {
	left   int64
	maxBuf int
}

func (r *boundedReader) Read(p []byte) (int, error) {
	r.maxBuf = max(r.maxBuf, len(p))
	if r.left == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.left))
	clear(p[:n])
	r.left -= int64(n)
	return n, nil
}

type putRecorder struct {
	storage.Backend
	sizes []int64
}

func (p *putRecorder) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	p.sizes = append(p.sizes, size)
	return p.Backend.Put(ctx, key, r, size)
}

func TestBlobPush_LargeBodyStreamsAndPutCarriesTheExactSize(t *testing.T) {
	e := newRegistryEnv(t, nil)
	rec := &putRecorder{Backend: mustLocal(t, e.objects)}
	e.svc.Registry.WithBackend(rec)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	const size = 100 << 20
	h := sha256.New()
	_, _ = io.Copy(h, &boundedReader{left: size})
	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	forget(t, e, digest)
	loc := e.startUpload(t, auth, e.pubImage)

	body := &boundedReader{left: size}
	rr := e.req(http.MethodPut, loc+"?digest="+digest, auth, body)

	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if body.maxBuf > 1<<20 {
		t.Errorf("the handler offered the body a %d byte buffer, want at most 1 MiB", body.maxBuf)
	}
	if len(rec.sizes) != 1 || rec.sizes[0] != size {
		t.Errorf("Put sizes = %v, want [%d]", rec.sizes, size)
	}
}

func mustLocal(t *testing.T, root string) storage.Backend {
	t.Helper()
	b, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestBlobPush_ConcurrentChunkCannotChangeABlobBeingCommitted(t *testing.T) {
	e := newRegistryEnv(t, nil)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	data := payload(t, "committed")
	loc := e.startUpload(t, auth, e.pubImage)
	e.req(http.MethodPatch, loc, auth, bytes.NewReader(data[:4]))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			e.req(http.MethodPatch, loc, auth, strings.NewReader("poison"))
		}
	}()
	rr := e.req(http.MethodPut, loc+"?digest="+digestOf(data), auth, bytes.NewReader(data[4:]))
	<-done

	if rr.Code == http.StatusCreated {
		got := e.req(http.MethodGet, "/v2/"+e.pubImage+"/blobs/"+digestOf(data), auth, nil)
		if digestOf(got.Body.Bytes()) != digestOf(data) {
			t.Errorf("the stored object does not match its digest")
		}
	}
}
