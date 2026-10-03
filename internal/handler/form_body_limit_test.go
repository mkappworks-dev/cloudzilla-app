package handler_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// countingReader records how many bytes of a request body were read.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// overFormCap is an upload past the router's form cap, kept under the 32 MB a
// multipart parse holds in memory so an uncapped parse leaves no temp file.
var overFormCap = bytes.Repeat([]byte("a"), handler.MaxNewFileBodyBytes+1<<20)

const formCapMsg = "request body is larger than 26 MB\n"

// postBrowserUpload posts the New file form as a signed-in browser does, with
// the session and CSRF cookies and the CSRF token as a field, and reports how
// many body bytes were read.
func postBrowserUpload(t *testing.T, api http.Handler, r raceRepo, file []byte) (*httptest.ResponseRecorder, int) {
	t.Helper()
	const csrfToken = "browser-csrf-token"
	contentType, form := uploadForm(t, url.Values{"csrf_token": {csrfToken}, "path": {"big.bin"}}, file)
	body := &countingReader{r: bytes.NewReader(form)}
	req := httptest.NewRequest(http.MethodPost, r.path+"/new/main", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: r.owner.token})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	rr := httptest.NewRecorder()
	api.ServeHTTP(rr, req)
	return rr, body.n
}

func TestRouter_RefusesAnOversizedAnonymousFormUnread(t *testing.T) {
	api := newAPIRouter(t, testutil.OpenTestDB(t))
	contentType, form := uploadForm(t, nil, overFormCap)
	body := &countingReader{r: bytes.NewReader(form)}
	req := httptest.NewRequest(http.MethodPost, "/no/such/route", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	api.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge || rr.Body.String() != formCapMsg {
		t.Errorf("want 413 %q, got %d %.100q", formCapMsg, rr.Code, rr.Body.String())
	}
	if body.n > handler.MaxNewFileBodyBytes+1 {
		t.Errorf("read %d body bytes, want at most %d", body.n, handler.MaxNewFileBodyBytes+1)
	}
}

// The upload's own 413 predates the form cap but comes after the whole body
// is read, so the bytes read tell the two apart.
func TestSubmitNewFile_RefusesAnOversizedBrowserUploadUnread(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)

	rr, read := postBrowserUpload(t, api, r, overFormCap)

	if rr.Code != http.StatusRequestEntityTooLarge || rr.Body.String() != formCapMsg {
		t.Errorf("want 413 %q, got %d %.100q", formCapMsg, rr.Code, rr.Body.String())
	}
	if got := branchHash(t, r.git, "main"); got != r.mainTip {
		t.Errorf("main = %s, want %s", got, r.mainTip)
	}
	if read > handler.MaxNewFileBodyBytes+1 {
		t.Errorf("read %d body bytes, want at most %d", read, handler.MaxNewFileBodyBytes+1)
	}
}
