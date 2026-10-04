package middleware

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const (
	testFormCap   = 1 << 10
	testCSRFToken = "test-csrf-token"
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

// formChain wraps route in the router's order: MaxFormBodySize, then CSRF.
func formChain(route http.HandlerFunc) http.Handler {
	return MaxFormBodySize(testFormCap)(CSRF(false)(route))
}

// formRequest builds a request whose body counts the bytes read from it. A
// non-empty token goes in the csrf_token cookie, as a browser holds it.
func formRequest(method, contentType string, body []byte, token string) (*http.Request, *countingReader) {
	counted := &countingReader{r: bytes.NewReader(body)}
	req := httptest.NewRequest(method, "/owner/repo/new/main", counted)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	}
	return req, counted
}

// multipartForm encodes an upload of data, after a csrf_token field unless
// token is empty.
func multipartForm(t *testing.T, token string, data []byte) (contentType string, body []byte) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if token != "" {
		if err := mw.WriteField("csrf_token", token); err != nil {
			t.Fatalf("write csrf_token: %v", err)
		}
	}
	fw, err := mw.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	return mw.FormDataContentType(), b.Bytes()
}

func urlencodedForm(token string, data []byte) []byte {
	return []byte(url.Values{"csrf_token": {token}, "content": {string(data)}}.Encode())
}

var overCap = bytes.Repeat([]byte("a"), 64*testFormCap)

func TestMaxFormBodySize_CSRFRefusesAFormOverTheCap(t *testing.T) {
	signedInType, signedInBody := multipartForm(t, testCSRFToken, overCap)
	anonymousType, anonymousBody := multipartForm(t, "", overCap)
	tests := []struct {
		name, method, contentType, token string
		body                             []byte
	}{
		{"multipart with the CSRF cookie and field", http.MethodPost, signedInType, testCSRFToken, signedInBody},
		{"urlencoded with the CSRF cookie and field", http.MethodPost, "application/x-www-form-urlencoded", testCSRFToken, urlencodedForm(testCSRFToken, overCap)},
		{"anonymous multipart", http.MethodPost, anonymousType, "", anonymousBody},
		{"anonymous multipart DELETE", http.MethodDelete, anonymousType, "", anonymousBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, body := formRequest(tt.method, tt.contentType, tt.body, tt.token)
			rec := httptest.NewRecorder()
			routeRan := false

			formChain(func(http.ResponseWriter, *http.Request) { routeRan = true }).ServeHTTP(rec, req)

			if want := "request body is larger than 1024 bytes\n"; rec.Code != http.StatusRequestEntityTooLarge || rec.Body.String() != want {
				t.Errorf("want 413 %q, got %d %q", want, rec.Code, rec.Body.String())
			}
			if routeRan {
				t.Error("the route ran")
			}
			if body.n > testFormCap+1 {
				t.Errorf("read %d body bytes, want at most %d", body.n, testFormCap+1)
			}
		})
	}
}

// Any Bearer header makes CSRF skip its parse, so the cap has to hold for
// whatever parses the form next.
func TestMaxFormBodySize_CapsTheRouteParseWhenCSRFSkipsIt(t *testing.T) {
	contentType, form := multipartForm(t, "", overCap)
	req, body := formRequest(http.MethodPost, contentType, form, "")
	req.Header.Set("Authorization", "Bearer x")
	var parseErr error

	formChain(func(_ http.ResponseWriter, r *http.Request) {
		parseErr = r.ParseMultipartForm(32 << 20)
	}).ServeHTTP(httptest.NewRecorder(), req)

	var tooLarge *http.MaxBytesError
	if !errors.As(parseErr, &tooLarge) {
		t.Errorf("route parse error = %v, want a MaxBytesError", parseErr)
	}
	if body.n > testFormCap+1 {
		t.Errorf("read %d body bytes, want at most %d", body.n, testFormCap+1)
	}
}

func TestMaxFormBodySize_LeavesASmallerRouteLimitInForce(t *testing.T) {
	const routeLimit = testFormCap / 2
	contentType, form := multipartForm(t, "", overCap)
	req, body := formRequest(http.MethodPost, contentType, form, "")
	req.Header.Set("Authorization", "Bearer x")
	var parseErr error
	route := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		parseErr = r.ParseMultipartForm(32 << 20)
	})

	MaxFormBodySize(testFormCap)(CSRF(false)(MaxBodySize(routeLimit)(route))).ServeHTTP(httptest.NewRecorder(), req)

	var tooLarge *http.MaxBytesError
	if !errors.As(parseErr, &tooLarge) || tooLarge.Limit != routeLimit {
		t.Errorf("route parse error = %v, want a MaxBytesError at %d", parseErr, routeLimit)
	}
	if body.n > routeLimit+1 {
		t.Errorf("read %d body bytes, want at most %d", body.n, routeLimit+1)
	}
}

// net/http still reads a urlencoded body whose media parameters don't parse.
func TestMaxFormBodySize_CapsAFormWithAnInvalidMediaParameter(t *testing.T) {
	req, body := formRequest(http.MethodPost, "application/x-www-form-urlencoded; charset", urlencodedForm(testCSRFToken, overCap), testCSRFToken)
	routeRan := false

	formChain(func(http.ResponseWriter, *http.Request) { routeRan = true }).ServeHTTP(httptest.NewRecorder(), req)

	if routeRan {
		t.Error("the route ran")
	}
	if body.n > testFormCap+1 {
		t.Errorf("read %d body bytes, want at most %d", body.n, testFormCap+1)
	}
}

func TestMaxFormBodySize_LeavesOtherBodiesWhole(t *testing.T) {
	tests := []struct{ name, path, contentType string }{
		{"git push", "/owner/repo.git/git-receive-pack", "application/x-git-receive-pack-request"},
		{"JSON", "/api/repos/owner/repo/issues", "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(overCap))
			req.Header.Set("Content-Type", tt.contentType)
			req.Header.Set("X-CSRF-Token", testCSRFToken)
			req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRFToken})
			var got []byte
			var readErr error

			formChain(func(_ http.ResponseWriter, r *http.Request) {
				got, readErr = io.ReadAll(r.Body)
			}).ServeHTTP(httptest.NewRecorder(), req)

			if readErr != nil || len(got) != len(overCap) {
				t.Errorf("route read %d bytes (error %v), want all %d", len(got), readErr, len(overCap))
			}
		})
	}
}

func TestMaxFormBodySize_PassesAFormUnderTheCap(t *testing.T) {
	multipartType, multipartBody := multipartForm(t, testCSRFToken, []byte("hello"))
	tests := []struct {
		name, contentType string
		body              []byte
	}{
		{"multipart", multipartType, multipartBody},
		{"urlencoded", "application/x-www-form-urlencoded", urlencodedForm(testCSRFToken, []byte("hello"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := formRequest(http.MethodPost, tt.contentType, tt.body, testCSRFToken)
			rec := httptest.NewRecorder()
			routeRan := false

			formChain(func(http.ResponseWriter, *http.Request) { routeRan = true }).ServeHTTP(rec, req)

			if !routeRan || rec.Code != http.StatusOK {
				t.Errorf("want the route to run, got %d %q", rec.Code, rec.Body.String())
			}
		})
	}
}
