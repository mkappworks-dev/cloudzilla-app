package handler_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSubmitNewFile_AuthorEmailFollowsKeepEmailPrivate(t *testing.T) {
	router, db, reposRoot := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, userID, owner, suffix)
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, repoName+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	headAuthor := func(path string) string {
		t.Helper()
		rr := postForm(t, router, token, "/"+owner+"/"+repoName+"/new/main",
			url.Values{"path": {path}, "content": {"hello\n"}, "message": {"Add " + path}})
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("commit %s: want 303, got %d: %s", path, rr.Code, rr.Body.String())
		}
		ref, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true)
		if err != nil {
			t.Fatalf("resolve main: %v", err)
		}
		c, err := gitRepo.CommitObject(ref.Hash())
		if err != nil {
			t.Fatalf("load head commit: %v", err)
		}
		if c.Author.Name != owner {
			t.Errorf("author name = %q, want %q", c.Author.Name, owner)
		}
		return c.Author.Email
	}

	wantNoreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, owner)
	if got := headAuthor("private.txt"); got != wantNoreply {
		t.Errorf("setting on: author email = %q, want %q", got, wantNoreply)
	}

	saveEmailSettings(t, router, token, url.Values{})
	if got, want := headAuthor("public.txt"), owner+"@test.invalid"; got != want {
		t.Errorf("setting off: author email = %q, want %q", got, want)
	}
}

func TestSubmitNewFile_RefusesPathCollisions(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	if rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"docs/guide.md"}, "content": {"guide\n"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("add docs/guide.md: want 303, got %d: %s", rr.Code, rr.Body.String())
	}

	tests := []struct{ name, path, want string }{
		{"file over a directory", "docs", "path collides with an existing entry: docs is a directory\n"},
		{"directory over a file", "a.txt/x", "path collides with an existing entry: a.txt is a file\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tip := branchHash(t, r.git, "main")
			rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {tt.path}, "content": {"x\n"}})
			if rr.Code != http.StatusConflict || rr.Body.String() != tt.want {
				t.Errorf("want 409 %q, got %d %q", tt.want, rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestUpdateProfileReadme_AuthorEmailFollowsKeepEmailPrivate(t *testing.T) {
	router, db, reposRoot := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	if _, err := db.Exec(
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $2, '', false, 'main')`, userID, owner); err != nil {
		t.Fatalf("seed profile repo: %v", err)
	}
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, owner+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	headAuthorEmail := func(content string) string {
		t.Helper()
		rr := postForm(t, router, token, "/settings/profile-readme", url.Values{"content": {content}})
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+owner {
			t.Fatalf("save README: want 303 to /%s, got %d %q: %s", owner, rr.Code, rr.Header().Get("Location"), rr.Body.String())
		}
		ref, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true)
		if err != nil {
			t.Fatalf("resolve main: %v", err)
		}
		c, err := gitRepo.CommitObject(ref.Hash())
		if err != nil {
			t.Fatalf("load head commit: %v", err)
		}
		return c.Author.Email
	}

	wantNoreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, owner)
	if got := headAuthorEmail("# hi\n"); got != wantNoreply {
		t.Errorf("setting on: author email = %q, want %q", got, wantNoreply)
	}

	saveEmailSettings(t, router, token, url.Values{})
	if got, want := headAuthorEmail("# hello\n"), owner+"@test.invalid"; got != want {
		t.Errorf("setting off: author email = %q, want %q", got, want)
	}
}

func TestUpdateProfileReadme_RefusesAnArchivedProfileRepo(t *testing.T) {
	router, db, reposRoot := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	if _, err := db.Exec(
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch, is_archived)
		 VALUES ($1, $2, $2, '', false, 'main', true)`, userID, owner); err != nil {
		t.Fatalf("seed profile repo: %v", err)
	}
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, owner+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	rr := postForm(t, router, token, "/settings/profile-readme", url.Values{"content": {"# hi\n"}})
	if want := "/" + owner + "?readme_error=profile_repo_archived"; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
		t.Fatalf("want 303 to %s, got %d %q", want, rr.Code, rr.Header().Get("Location"))
	}
	if _, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true); err == nil {
		t.Error("an archived profile repo got a commit")
	}
}

// uploadCap is the largest file the New file form commits.
const uploadCap = 25 << 20

// uploadForm encodes the New file form as a browser does: multipart, with file
// as the upload unless it is nil.
func uploadForm(t *testing.T, fields url.Values, file []byte) (contentType string, body []byte) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for name, values := range fields {
		for _, v := range values {
			if err := mw.WriteField(name, v); err != nil {
				t.Fatalf("write field %s: %v", name, err)
			}
		}
	}
	if file != nil {
		fw, err := mw.CreateFormFile("file", "upload.bin")
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := fw.Write(file); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	return mw.FormDataContentType(), b.Bytes()
}

// postUpload posts the New file form with a Bearer token.
func postUpload(t *testing.T, router http.Handler, token, path string, fields url.Values, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	contentType, body := uploadForm(t, fields, file)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestSubmitNewFile_CommitsAnUploadAtTheCap(t *testing.T) {
	atCap := bytes.Repeat([]byte("a"), uploadCap)
	tests := []struct {
		name string
		send func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder
	}{
		{"Bearer token", func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder {
			return postUpload(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"big.bin"}}, atCap)
		}},
		{"browser session", func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder {
			rr, _ := postBrowserUpload(t, api, r, atCap)
			return rr
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)

			rr := tt.send(t, api, r)

			if rr.Code != http.StatusSeeOther {
				t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
			}
			tip, err := r.git.CommitObject(branchHash(t, r.git, "main"))
			if err != nil {
				t.Fatalf("load main: %v", err)
			}
			f, err := tip.File("big.bin")
			if err != nil {
				t.Fatalf("find big.bin: %v", err)
			}
			if f.Size != uploadCap {
				t.Errorf("big.bin is %d bytes, want %d", f.Size, uploadCap)
			}
		})
	}
}

func TestSubmitNewFile_RefusesOversizedForms(t *testing.T) {
	overLimit := url.Values{"path": {"big.txt"}, "content": {strings.Repeat("a", 27<<20)}}
	tests := []struct {
		name string
		send func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder
	}{
		{"upload over the cap", func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder {
			return postUpload(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"big.bin"}}, bytes.Repeat([]byte("a"), uploadCap+1))
		}},
		{"multipart body over the limit", func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder {
			return postUpload(t, api, r.owner.token, r.path+"/new/main", overLimit, nil)
		}},
		{"urlencoded body over the limit", func(t *testing.T, api http.Handler, r raceRepo) *httptest.ResponseRecorder {
			return postForm(t, api, r.owner.token, r.path+"/new/main", overLimit)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)

			rr := tt.send(t, api, r)

			if want := "files are limited to 25 MB\n"; rr.Code != http.StatusRequestEntityTooLarge || rr.Body.String() != want {
				t.Errorf("want 413 %q, got %d %.100q", want, rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "main"); got != r.mainTip {
				t.Errorf("main = %s, want %s", got, r.mainTip)
			}
		})
	}
}

func TestSubmitNewFile_RefusesInvalidPathsWithoutLoggingThem(t *testing.T) {
	tests := []struct{ name, path string }{
		{"too long", strings.Repeat("d/", 2048) + "f"},
		{".git", ".git/hooks/post-checkout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)
			var logs bytes.Buffer
			defaultLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(defaultLogger) })

			rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {tt.path}, "content": {"hook\n"}})

			if rr.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(rr.Body.String(), "invalid file path: ") {
				t.Errorf("want 422 invalid file path, got %d %.100q", rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "main"); got != r.mainTip {
				t.Errorf("main = %s, want %s", got, r.mainTip)
			}
			if strings.Contains(logs.String(), "commit file failed") {
				t.Errorf("a refused path was logged as a failure:\n%.300s", logs.String())
			}
		})
	}
}

func TestSubmitNewFile_UploadKeepsItsOwnName(t *testing.T) {
	tests := []struct{ name, path, dir, want string }{
		{"existing directory", "docs/", "", "docs/upload.bin"},
		{"new directory", "notes/2026/", "", "notes/2026/upload.bin"},
		{"root", "/", "", "upload.bin"},
		{"blank path from a directory page", "", "docs", "docs/upload.bin"},
		{"blank path from the root page", "", "", "upload.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)
			if rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"docs/guide.md"}, "content": {"guide\n"}}); rr.Code != http.StatusSeeOther {
				t.Fatalf("add docs/guide.md: want 303, got %d: %s", rr.Code, rr.Body.String())
			}

			rr := postUpload(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {tt.path}, "dir": {tt.dir}}, []byte("uploaded\n"))

			if want := r.path + "/blob/main/" + tt.want; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
				t.Fatalf("want 303 to %s, got %d %q: %s", want, rr.Code, rr.Header().Get("Location"), rr.Body.String())
			}
			tip, err := r.git.CommitObject(branchHash(t, r.git, "main"))
			if err != nil {
				t.Fatalf("load main: %v", err)
			}
			if want := "Create " + tt.want; tip.Message != want {
				t.Errorf("commit message = %q, want %q", tip.Message, want)
			}
			for path, want := range map[string]string{tt.want: "uploaded\n", "docs/guide.md": "guide\n"} {
				f, err := tip.File(path)
				if err != nil {
					t.Errorf("find %s: %v", path, err)
					continue
				}
				if got, err := f.Contents(); err != nil || got != want {
					t.Errorf("%s = %q (%v), want %q", path, got, err, want)
				}
			}
		})
	}
}

func TestSubmitNewFile_AsksForAFileNameAfterATrailingSlash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	if rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"docs/guide.md"}, "content": {"guide\n"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("add docs/guide.md: want 303, got %d: %s", rr.Code, rr.Body.String())
	}

	tests := []struct{ name, path string }{
		{"existing directory", "docs/"},
		{"new directory", "notes/"},
		{"root", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tip := branchHash(t, r.git, "main")

			rr := postUpload(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {tt.path}, "content": {"typed\n"}}, nil)

			if want := "the path ends in /: add a file name or upload a file\n"; rr.Code != http.StatusBadRequest || rr.Body.String() != want {
				t.Errorf("want 400 %q, got %d %q", want, rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}
