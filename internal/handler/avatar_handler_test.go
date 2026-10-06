package handler_test

// Router-level tests for avatar upload and serving. They require TEST_DATABASE_DSN.

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
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

type avatarRouterEnv struct {
	db      *sql.DB
	svc     *service.Services
	router  http.Handler
	backend storage.Backend
}

func newAvatarRouterEnv(t *testing.T) avatarRouterEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
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
	return avatarRouterEnv{db: db, svc: svc, router: h, backend: backend}
}

func avatarPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multipartBody builds a form with the file under "avatar", declared with the
// given filename and Content-Type, plus any extra fields.
func multipartBody(t *testing.T, filename, contentType string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="avatar"; filename="`+filename+`"`)
	hdr.Set("Content-Type", contentType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

type uploadOpts struct {
	token       string
	htmx        bool
	cookieAuth  bool // cookie session with the CSRF token as a form field, as a plain HTML form sends it
	filename    string
	contentType string
}

func (e avatarRouterEnv) upload(t *testing.T, path string, data []byte, o uploadOpts) *httptest.ResponseRecorder {
	t.Helper()
	if o.filename == "" {
		o.filename = "me.png"
	}
	if o.contentType == "" {
		o.contentType = "image/png"
	}
	fields := map[string]string{}
	if o.cookieAuth {
		fields["csrf_token"] = "csrf-test-token"
	}
	body, ct := multipartBody(t, o.filename, o.contentType, data, fields)
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", ct)
	switch {
	case o.cookieAuth:
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: o.token})
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "csrf-test-token"})
	case o.token != "":
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	if o.htmx {
		req.Header.Set("HX-Request", "true")
		if o.cookieAuth {
			req.Header.Set("X-CSRF-Token", "csrf-test-token")
		}
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e avatarRouterEnv) post(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e avatarRouterEnv) get(t *testing.T, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e avatarRouterEnv) userKey(t *testing.T, id int64) string {
	t.Helper()
	u, err := e.svc.User.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.AvatarKey
}

func TestUserAvatar_UploadServeAndRemove(t *testing.T) {
	e := newAvatarRouterEnv(t)
	user := seedSignedInUser(t, e.db)

	rr := e.upload(t, "/settings/avatar", avatarPNG(t, color.NRGBA{200, 0, 0, 255}), uploadOpts{token: user.token})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings" {
		t.Fatalf("upload: got %d to %q; body %.200s", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
	key := e.userKey(t, user.id)
	if !strings.HasPrefix(key, "avatars/user/") {
		t.Fatalf("key = %q", key)
	}

	got := e.get(t, "/"+key, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("serve: %d", got.Code)
	}
	for header, want := range map[string]string{
		"Content-Type":            "image/jpeg",
		"Cache-Control":           "public, max-age=31536000, immutable",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if v := got.Header().Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	etag := got.Header().Get("ETag")
	if hash := strings.TrimSuffix(key[strings.LastIndex(key, "/")+1:], ".jpg"); etag != `"`+hash+`"` {
		t.Errorf("ETag = %q, want the content hash %q", etag, hash)
	}
	if _, _, err := image.Decode(got.Body); err != nil {
		t.Errorf("served body is not an image: %v", err)
	}
	if notMod := e.get(t, "/"+key, map[string]string{"If-None-Match": etag}); notMod.Code != http.StatusNotModified || notMod.Body.Len() != 0 {
		t.Errorf("If-None-Match: got %d with %d bytes, want an empty 304", notMod.Code, notMod.Body.Len())
	}

	if rr := e.post(t, "/settings/avatar/delete", user.token); rr.Code != http.StatusSeeOther {
		t.Fatalf("remove: %d", rr.Code)
	}
	if e.userKey(t, user.id) != "" {
		t.Error("remove left the key")
	}
	if gone := e.get(t, "/"+key, nil); gone.Code != http.StatusNotFound || gone.Header().Get("Cache-Control") == "public, max-age=31536000, immutable" {
		t.Errorf("removed avatar: got %d, Cache-Control %q", gone.Code, gone.Header().Get("Cache-Control"))
	}
}

func TestUserAvatar_RejectsOverCap(t *testing.T) {
	e := newAvatarRouterEnv(t)
	user := seedSignedInUser(t, e.db)
	big := append(avatarPNG(t, color.White), make([]byte, 3<<20)...)

	t.Run("bearer", func(t *testing.T) {
		if rr := e.upload(t, "/settings/avatar", big, uploadOpts{token: user.token}); rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("got %d, want 413", rr.Code)
		}
	})
	// CSRF parses this form under the global form cap before the route's own
	// limit applies, so the image size check has to catch it.
	t.Run("cookie form without X-CSRF-Token", func(t *testing.T) {
		if rr := e.upload(t, "/settings/avatar", big, uploadOpts{token: user.token, cookieAuth: true}); rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("got %d, want 413; body %.200s", rr.Code, rr.Body.String())
		}
	})
	t.Run("htmx shows the error inline", func(t *testing.T) {
		rr := e.upload(t, "/settings/avatar", big, uploadOpts{token: user.token, cookieAuth: true, htmx: true})
		if rr.Header().Get("HX-Retarget") != "#avatar-form-error" || !strings.Contains(rr.Body.String(), "at most 2 MB") {
			t.Errorf("got %d, HX-Retarget %q, body %.200s", rr.Code, rr.Header().Get("HX-Retarget"), rr.Body.String())
		}
	})
	if e.userKey(t, user.id) != "" {
		t.Error("an oversized upload set the key")
	}
}

func TestUserAvatar_RejectsNonImages(t *testing.T) {
	e := newAvatarRouterEnv(t)
	user := seedSignedInUser(t, e.db)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	cases := map[string]uploadOpts{
		"svg declared as png":   {filename: "me.png", contentType: "image/png"},
		"svg named svg":         {filename: "me.svg", contentType: "image/svg+xml"},
		"svg named jpg as jpeg": {filename: "me.jpg", contentType: "image/jpeg"},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			o.token = user.token
			if rr := e.upload(t, "/settings/avatar", svg, o); rr.Code != http.StatusUnprocessableEntity {
				t.Errorf("got %d, want 422", rr.Code)
			}
		})
	}
	t.Run("htmx", func(t *testing.T) {
		rr := e.upload(t, "/settings/avatar", svg, uploadOpts{token: user.token, cookieAuth: true, htmx: true})
		if !strings.Contains(rr.Body.String(), "Use a PNG, JPEG, GIF or WebP image.") {
			t.Errorf("body %.200s", rr.Body.String())
		}
	})
	// A real PNG is accepted whatever its name and declared type.
	if rr := e.upload(t, "/settings/avatar", avatarPNG(t, color.White), uploadOpts{token: user.token, filename: "me.svg", contentType: "image/svg+xml"}); rr.Code != http.StatusSeeOther {
		t.Errorf("png named .svg: got %d, want 303", rr.Code)
	}
}

func TestUserAvatar_SignedOut(t *testing.T) {
	e := newAvatarRouterEnv(t)
	seedSignedInUser(t, e.db) // setup counts as done once an account exists
	body, ct := multipartBody(t, "me.png", "image/png", avatarPNG(t, color.White), nil)
	req := httptest.NewRequest(http.MethodPost, "/settings/avatar", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", "csrf-test-token")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "csrf-test-token"})
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	switch {
	case rr.Code == http.StatusUnauthorized:
	case rr.Code == http.StatusSeeOther && strings.HasPrefix(rr.Header().Get("Location"), "/login"):
	default:
		t.Errorf("got %d to %q, want 401 or a redirect to sign in", rr.Code, rr.Header().Get("Location"))
	}
}

func TestOrgAvatar_OwnerOnly(t *testing.T) {
	e := newAvatarRouterEnv(t)
	ctx := context.Background()
	owner := seedSignedInUser(t, e.db)
	member := seedSignedInUser(t, e.db)
	org, err := e.svc.Org.Create(ctx, owner.id, "avorg-"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	if err := e.svc.Org.AddMember(ctx, org.ID, owner.id, member.id, model.OrgRoleMember); err != nil {
		t.Fatal(err)
	}
	path := "/orgs/" + org.Name + "/settings/avatar"
	img := avatarPNG(t, color.NRGBA{0, 100, 0, 255})

	if rr := e.upload(t, path, img, uploadOpts{token: member.token}); rr.Code != http.StatusForbidden {
		t.Errorf("member upload: got %d, want 403", rr.Code)
	}
	if rr := e.upload(t, "/orgs/no-such-org-"+testutil.UniqueSuffix(t)+"/settings/avatar", img, uploadOpts{token: owner.token}); rr.Code != http.StatusNotFound {
		t.Errorf("unknown org: got %d, want 404", rr.Code)
	}
	// Larger than /api/orgs' 1 MB limit, which these routes sit outside.
	padded := append(avatarPNG(t, color.White), make([]byte, 1200<<10)...)
	if rr := e.upload(t, path, padded, uploadOpts{token: owner.token}); rr.Code != http.StatusSeeOther {
		t.Fatalf("owner upload over 1 MB: got %d; body %.200s", rr.Code, rr.Body.String())
	}
	got, err := e.svc.Org.Get(ctx, org.Name)
	if err != nil || !strings.HasPrefix(got.AvatarKey, "avatars/org/") {
		t.Fatalf("org key = %q, %v", got.AvatarKey, err)
	}
	if rr := e.post(t, path+"/delete", member.token); rr.Code != http.StatusForbidden {
		t.Errorf("member remove: got %d, want 403", rr.Code)
	}
	// AuditService.Record writes in the background.
	var audits int
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = $1 AND target_id = $2`, model.AuditActionOrgAvatarUpdate, org.ID).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if audits > 0 {
			break
		}
	}
	if audits != 1 {
		t.Errorf("audit rows = %d, want 1", audits)
	}
}

func TestServeAvatar_RejectsMalformedAndUnknownKeys(t *testing.T) {
	e := newAvatarRouterEnv(t)
	seedSignedInUser(t, e.db) // setup counts as done once an account exists
	hash := strings.Repeat("a", 64)
	for _, path := range []string{
		"/avatars/user/1/" + hash + ".png",
		"/avatars/user/1/" + hash + ".svg",
		"/avatars/user/x/" + hash + ".png",
		"/avatars/repo/1/" + hash + ".png",
		"/avatars/user/1/" + strings.Repeat("A", 64) + ".png",
		"/avatars/user/1/short.png",
		"/avatars/user/1/../../etc/passwd",
		"/avatars/",
	} {
		rr := e.get(t, path, nil)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", path, rr.Code)
		}
		body, _ := io.ReadAll(rr.Body)
		if strings.Contains(string(body), "root:") {
			t.Errorf("%s leaked a file", path)
		}
	}
}

func (e avatarRouterEnv) page(t *testing.T, path, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: token})
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, rr.Code)
	}
	return rr.Body.String()
}

func TestAvatar_RendersOnIdentitySurfaces(t *testing.T) {
	e := newAvatarRouterEnv(t)
	ctx := context.Background()
	user := seedSignedInUser(t, e.db)
	other := seedSignedInUser(t, e.db)
	org, err := e.svc.Org.Create(ctx, user.id, "avorg-"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	if rr := e.upload(t, "/settings/avatar", avatarPNG(t, color.NRGBA{1, 2, 3, 255}), uploadOpts{token: user.token}); rr.Code != http.StatusSeeOther {
		t.Fatalf("upload: %d", rr.Code)
	}
	if rr := e.upload(t, "/orgs/"+org.Name+"/settings/avatar", avatarPNG(t, color.NRGBA{4, 5, 6, 255}), uploadOpts{token: user.token}); rr.Code != http.StatusSeeOther {
		t.Fatalf("org upload: %d", rr.Code)
	}
	userSrc := `src="/` + e.userKey(t, user.id) + `"`
	gotOrg, err := e.svc.Org.Get(ctx, org.Name)
	if err != nil {
		t.Fatal(err)
	}
	orgSrc := `src="/` + gotOrg.AvatarKey + `"`

	settings := e.page(t, "/settings", user.token)
	assertContains(t, settings, userSrc)
	if strings.Count(settings, userSrc) < 2 {
		t.Error("settings: want the avatar in both the nav and the profile block")
	}
	assertContains(t, e.page(t, "/"+user.name, other.token), userSrc)
	assertContains(t, e.page(t, "/"+user.name, ""), orgSrc) // org list on the profile
	orgPage := e.page(t, "/"+org.Name, other.token)
	assertContains(t, orgPage, orgSrc)
	assertContains(t, orgPage, userSrc) // member list
	assertContains(t, e.page(t, "/orgs/"+org.Name+"/settings", user.token), orgSrc)
	assertContains(t, e.page(t, "/organizations", user.token), orgSrc)

	// The other user has none and keeps initials.
	if strings.Contains(e.page(t, "/"+other.name, other.token), `src="/avatars/user/`+strconv.FormatInt(other.id, 10)) {
		t.Error("a user without an avatar rendered an image")
	}

	api := e.get(t, "/api/users/"+user.name, nil)
	assertContains(t, api.Body.String(), `"avatar_url":"http://localhost:8080/`+e.userKey(t, user.id)+`"`)
	orgAPI := e.get(t, "/api/orgs/"+org.Name, nil)
	assertContains(t, orgAPI.Body.String(), `"avatar_url":"http://localhost:8080/`+gotOrg.AvatarKey+`"`)
}

func TestAvatar_RendersOnActivitySurfaces(t *testing.T) {
	e := newAvatarRouterEnv(t)
	ctx := context.Background()
	author := seedSignedInUser(t, e.db)
	commenter := seedSignedInUser(t, e.db)
	viewer := seedSignedInUser(t, e.db) // no avatar, so the nav can't satisfy an assertion
	for _, u := range []signedInUser{author, commenter} {
		if rr := e.upload(t, "/settings/avatar", avatarPNG(t, color.NRGBA{uint8(u.id), 9, 9, 255}), uploadOpts{token: u.token}); rr.Code != http.StatusSeeOther {
			t.Fatalf("upload for %s: %d", u.name, rr.Code)
		}
	}
	authorSrc := `src="/` + e.userKey(t, author.id) + `"`
	commenterSrc := `src="/` + e.userKey(t, commenter.id) + `"`

	repo, err := e.svc.Repo.Create(ctx, author.id, author.name, "avrepo", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	issue, err := e.svc.Issue.Create(ctx, author.name, repo.Name, author.id, "Avatars", "", "")
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if _, err := e.svc.Comment.CreateForIssue(ctx, *repo, issue.ID, issue.Number, commenter.id, commenter.name, "hello"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if err := e.svc.Assignee.AddToIssue(ctx, author.name, repo.Name, issue.Number, author.name); err != nil {
		t.Fatalf("assign: %v", err)
	}
	pull := &model.PullRequest{RepoID: repo.ID, AuthorID: author.id, Title: "Avatars", State: model.PRStateOpen, HeadBranch: "main", BaseBranch: "main"}
	if err := store.NewPullStore(e.db).Create(ctx, pull); err != nil {
		t.Fatalf("create pull: %v", err)
	}
	if _, err := e.svc.Comment.CreateForPull(ctx, *repo, pull.ID, pull.Number, commenter.id, commenter.name, "lgtm"); err != nil {
		t.Fatalf("pull comment: %v", err)
	}
	categories, err := e.svc.Discussion.ListCategories(ctx)
	if err != nil || len(categories) == 0 {
		t.Fatalf("categories: %v", err)
	}
	discussion, err := e.svc.Discussion.Create(ctx, author.name, repo.Name, commenter.id, commenter.name, categories[0].ID, "Question", "body")
	if err != nil {
		t.Fatalf("discussion: %v", err)
	}
	if _, err := e.svc.Gist.Create(ctx, author.id, author.name, "avatar gist "+testutil.UniqueSuffix(t), true, []model.GistFile{{Filename: "a.txt", Content: "x"}}); err != nil {
		t.Fatalf("gist: %v", err)
	}

	repoPath := "/" + author.name + "/" + repo.Name
	issuePage := e.page(t, repoPath+"/issues/"+strconv.Itoa(issue.Number), viewer.token)
	assertContains(t, issuePage, authorSrc)    // issue author and assignee
	assertContains(t, issuePage, commenterSrc) // comment author
	pullPage := e.page(t, repoPath+"/pulls/"+strconv.Itoa(pull.Number), viewer.token)
	assertContains(t, pullPage, authorSrc)
	assertContains(t, pullPage, commenterSrc)
	assertContains(t, e.page(t, repoPath+"/discussions/"+strconv.Itoa(discussion.Number), viewer.token), commenterSrc)
	assertContains(t, e.page(t, repoPath+"/discussions", viewer.token), commenterSrc)
	assertContains(t, e.page(t, "/gists", viewer.token), authorSrc)

	// The htmx comment fragment carries the new comment's avatar too.
	form := strings.NewReader("body=more")
	req := httptest.NewRequest(http.MethodPost, "/api/repos"+repoPath+"/issues/"+strconv.Itoa(issue.Number)+"/comments", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+commenter.token)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("post comment: %d %.200s", rr.Code, rr.Body.String())
	}
	assertContains(t, rr.Body.String(), commenterSrc)
}
