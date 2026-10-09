package handler_test

// Router-level tests for markdown image attachments. They require TEST_DATABASE_DSN.

import (
	"bytes"
	"encoding/json"
	"image/color"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
)

type uploadedAttachment struct {
	URL      string `json:"url"`
	Markdown string `json:"markdown"`
}

func attachmentRequest(t *testing.T, e avatarRouterEnv, path, token, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	hdr.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(hdr)
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

func (e avatarRouterEnv) uploadAttachment(t *testing.T, repo seededRepo, token string) uploadedAttachment {
	t.Helper()
	rr := attachmentRequest(t, e, repo.path+"/attachments", token, "Screen Shot.png", avatarPNG(t, color.NRGBA{10, 20, 30, 255}))
	if rr.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rr.Code, rr.Body.String())
	}
	var got uploadedAttachment
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAttachment_UploadReturnsMarkdownAndServesToAnyoneOnAPublicRepo(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	up := e.uploadAttachment(t, repo, repo.owner.token)

	if !strings.HasPrefix(up.URL, "/attachments/") || !strings.HasSuffix(up.URL, ".png") {
		t.Fatalf("url = %q", up.URL)
	}
	if want := "![Screen Shot](" + up.URL + ")"; up.Markdown != want {
		t.Errorf("markdown = %q, want %q", up.Markdown, want)
	}

	got := e.get(t, up.URL, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("anonymous GET on a public repo: %d", got.Code)
	}
	for header, want := range map[string]string{
		"Content-Type":            "image/png",
		"Cache-Control":           "private, no-cache",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"Vary":                    "Authorization, Cookie",
	} {
		if v := got.Header().Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	etag := got.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if !bytes.Equal(got.Body.Bytes(), avatarPNG(t, color.NRGBA{10, 20, 30, 255})) {
		t.Error("served bytes differ from the upload")
	}
	if again := e.get(t, up.URL, map[string]string{"If-None-Match": etag}); again.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d, want 304", again.Code)
	}
	req := httptest.NewRequest(http.MethodHead, up.URL, nil)
	head := httptest.NewRecorder()
	e.router.ServeHTTP(head, req)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD: %d with %d body bytes", head.Code, head.Body.Len())
	}
}

func TestAttachment_PrivateRepoFollowsCanRead(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, true)
	stranger := seedSignedInUser(t, e.db)
	up := e.uploadAttachment(t, repo, repo.owner.token)

	if rr := e.get(t, up.URL, map[string]string{"Authorization": "Bearer " + repo.owner.token}); rr.Code != http.StatusOK {
		t.Errorf("owner: %d, want 200", rr.Code)
	}
	if rr := e.get(t, up.URL, nil); rr.Code != http.StatusNotFound {
		t.Errorf("anonymous: %d, want 404", rr.Code)
	}
	if rr := e.get(t, up.URL, map[string]string{"Authorization": "Bearer " + stranger.token}); rr.Code != http.StatusNotFound {
		t.Errorf("signed-in non-collaborator: %d, want 404", rr.Code)
	}
	// A matching ETag must not turn into a 304 for someone who can't read it.
	etag := e.get(t, up.URL, map[string]string{"Authorization": "Bearer " + repo.owner.token}).Header().Get("ETag")
	if rr := e.get(t, up.URL, map[string]string{"If-None-Match": etag}); rr.Code != http.StatusNotFound {
		t.Errorf("anonymous with the owner's ETag: %d, want 404", rr.Code)
	}

	if rr := attachmentRequest(t, e, repo.path+"/attachments", stranger.token, "a.png", avatarPNG(t, color.NRGBA{1, 1, 1, 255})); rr.Code != http.StatusNotFound {
		t.Errorf("upload to an unreadable repo: %d, want 404", rr.Code)
	}
}

func TestAttachment_RepoMadePrivateStopsServingAnonymous(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	up := e.uploadAttachment(t, repo, repo.owner.token)
	if rr := e.get(t, up.URL, nil); rr.Code != http.StatusOK {
		t.Fatalf("before: %d", rr.Code)
	}
	if _, err := e.db.Exec(`UPDATE repositories SET private = true WHERE id = $1`, repo.id); err != nil {
		t.Fatal(err)
	}
	if rr := e.get(t, up.URL, nil); rr.Code != http.StatusNotFound {
		t.Errorf("after making the repo private: %d, want 404", rr.Code)
	}
}

func TestAttachment_UploadRejections(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	path := repo.path + "/attachments"
	png := avatarPNG(t, color.NRGBA{5, 5, 5, 255})

	// CSRF turns a credential-less POST away before the handler runs.
	if rr := attachmentRequest(t, e, path, "", "a.png", png); rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
		t.Errorf("signed out: %d, want 401 or 403", rr.Code)
	}
	if rr := attachmentRequest(t, e, path, repo.owner.token, "a.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("svg: %d, want 422", rr.Code)
	}
	huge := append(append([]byte{}, png...), bytes.Repeat([]byte{0}, attachment.MaxBytes)...)
	if rr := attachmentRequest(t, e, path, repo.owner.token, "a.png", huge); rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over 10 MB: %d, want 413", rr.Code)
	}
	if rr := attachmentRequest(t, e, "/nobody/nothing/attachments", repo.owner.token, "a.png", png); rr.Code != http.StatusNotFound {
		t.Errorf("unknown repo: %d, want 404", rr.Code)
	}
}

func TestAttachment_ServeRejectsMalformedAndUnknownNames(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	up := e.uploadAttachment(t, repo, repo.owner.token)
	token := strings.TrimSuffix(strings.TrimPrefix(up.URL, "/attachments/"), ".png")

	for _, name := range []string{
		strings.Repeat("0", 32) + ".png", // unknown token
		token + ".jpg",                   // wrong extension for the stored object
		token,                            // no extension
		token + ".svg",
		strings.ToUpper(token) + ".png",
		"..%2f" + token + ".png",
	} {
		if rr := e.get(t, "/attachments/"+name, nil); rr.Code != http.StatusNotFound {
			t.Errorf("GET /attachments/%s: %d, want 404", name, rr.Code)
		}
	}
}

func TestAttachment_SoftDeletedRepoStopsServingAndRestoreBringsItBack(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	up := e.uploadAttachment(t, repo, repo.owner.token)

	if _, err := e.db.Exec(`UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, repo.id); err != nil {
		t.Fatal(err)
	}
	if rr := e.get(t, up.URL, nil); rr.Code != http.StatusNotFound {
		t.Errorf("soft-deleted repo: %d, want 404", rr.Code)
	}
	if _, err := e.db.Exec(`UPDATE repositories SET deleted_at = NULL WHERE id = $1`, repo.id); err != nil {
		t.Fatal(err)
	}
	if rr := e.get(t, up.URL, nil); rr.Code != http.StatusOK {
		t.Errorf("restored repo: %d, want 200", rr.Code)
	}
}

func TestAttachment_EditorsGetTheUploadURLExceptOnTheWiki(t *testing.T) {
	e := newAvatarRouterEnv(t)
	repo := seedOwnedRepo(t, e.db, false)
	auth := map[string]string{"Authorization": "Bearer " + repo.owner.token}
	attr := `data-attachments-url="` + repo.path + `/attachments"`

	issue := e.get(t, repo.path+"/issues/new", auth)
	if issue.Code != http.StatusOK || !strings.Contains(issue.Body.String(), attr) {
		t.Errorf("new issue page: %d, has %s: %v", issue.Code, attr, strings.Contains(issue.Body.String(), attr))
	}
	if rr := e.get(t, repo.path+"/wiki/new", auth); rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "data-attachments-url") {
		t.Error("the wiki editor offers uploads, but wiki pages live in git where the sweep can't see their references")
	}
	if rr := e.get(t, repo.path+"/issues", nil); strings.Contains(rr.Body.String(), "data-attachments-url") {
		t.Error("a signed-out page offers uploads")
	}
}
