package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// editRepo is a raceRepo whose main also holds an executable, a symlink, a
// binary file, a file over the editor's cap, a CRLF file, and docs/guide.md.
type editRepo struct {
	raceRepo
	api  http.Handler
	db   *sql.DB
	code *service.CodeService
}

const editCap = 1 << 20

func seedEditRepo(t *testing.T) editRepo {
	t.Helper()
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	r := editRepo{raceRepo: seedRaceRepo(t, db, reposRoot), api: newAPIRouterAt(t, db, reposRoot), db: db, code: code}
	for path, content := range map[string]string{
		"docs/guide.md": "guide\n",
		"bin.dat":       "a\x00b",
		"big.txt":       strings.Repeat("a", editCap+1),
		"crlf.txt":      "one\r\ntwo\r\n",
	} {
		if err := code.CommitFile(r.owner.name, r.name, "main", path, []byte(content), raceAuthor, "Add "+path); err != nil {
			t.Fatalf("commit %s: %v", path, err)
		}
	}
	setRootEntry(t, r.git, "main", "run.sh", filemode.Executable, "echo\n")
	setRootEntry(t, r.git, "main", "link", filemode.Symlink, "a.txt")
	if err := code.CreateTag(r.owner.name, r.name, "v1", "main"); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	return r
}

// setRootEntry commits content at the root-level name on branch with mode.
func setRootEntry(t *testing.T, git *gogit.Repository, branch, name string, mode filemode.FileMode, content string) {
	t.Helper()
	store := func(encode func(plumbing.EncodedObject) error) plumbing.Hash {
		obj := git.Storer.NewEncodedObject()
		if err := encode(obj); err != nil {
			t.Fatalf("encode: %v", err)
		}
		h, err := git.Storer.SetEncodedObject(obj)
		if err != nil {
			t.Fatalf("store: %v", err)
		}
		return h
	}
	blob := store(func(o plumbing.EncodedObject) error {
		o.SetType(plumbing.BlobObject)
		w, err := o.Writer()
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, content); err != nil {
			return err
		}
		return w.Close()
	})
	parent, err := git.CommitObject(branchHash(t, git, branch))
	if err != nil {
		t.Fatalf("load %s: %v", branch, err)
	}
	tree, err := parent.Tree()
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	entries := []object.TreeEntry{{Name: name, Mode: mode, Hash: blob}}
	for _, e := range tree.Entries {
		if e.Name != name {
			entries = append(entries, e)
		}
	}
	sort.Sort(object.TreeEntrySorter(entries))
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Now()}
	commit := store((&object.Commit{
		Author: sig, Committer: sig, Message: "Set " + name,
		TreeHash:     store((&object.Tree{Entries: entries}).Encode),
		ParentHashes: []plumbing.Hash{parent.Hash},
	}).Encode)
	if err := git.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), commit)); err != nil {
		t.Fatalf("move %s: %v", branch, err)
	}
}

func (r editRepo) file(t *testing.T, branch, path string) (*object.File, bool) {
	t.Helper()
	c, err := r.git.CommitObject(branchHash(t, r.git, branch))
	if err != nil {
		t.Fatalf("load %s: %v", branch, err)
	}
	f, err := c.File(path)
	if err != nil {
		return nil, false
	}
	return f, true
}

func (r editRepo) contents(t *testing.T, path string) string {
	t.Helper()
	f, ok := r.file(t, "main", path)
	if !ok {
		t.Fatalf("%s is not on main", path)
	}
	s, err := f.Contents()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return s
}

func (r editRepo) blobSHA(t *testing.T, path string) string {
	t.Helper()
	f, ok := r.file(t, "main", path)
	if !ok {
		t.Fatalf("%s is not on main", path)
	}
	return f.Hash.String()
}

func (r editRepo) headMessage(t *testing.T) string {
	t.Helper()
	c, err := r.git.CommitObject(branchHash(t, r.git, "main"))
	if err != nil {
		t.Fatalf("load main: %v", err)
	}
	return c.Message
}

// send makes a request with token as a Bearer token (none when empty).
func send(t *testing.T, h http.Handler, method, token, path string, form url.Values, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// crlf spells content as a browser submits a textarea holding it.
func crlf(content string) string { return strings.ReplaceAll(content, "\n", "\r\n") }

func TestEditFile_PageShowsTheFile(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "lead.txt", "\nafter a blank line\n")
	rr := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/edit/main/lead.txt", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %.300s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		">\n\nafter a blank line\n</textarea>",
		`name="blob_sha" value="` + r.blobSHA(t, "lead.txt") + `"`,
		`value="lead.txt"`,
		`action="` + r.path + `/edit/main/lead.txt"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func (r editRepo) commitMain(t *testing.T, path, content string) {
	t.Helper()
	if err := r.code.CommitFile(r.owner.name, r.name, "main", path, []byte(content), raceAuthor, "Add "+path); err != nil {
		t.Fatalf("commit %s: %v", path, err)
	}
}

func TestEditFile_CommitsTheEdit(t *testing.T) {
	tests := []struct {
		name, path, typed, want string
	}{
		{"LF file", "a.txt", crlf("one\nthree\n"), "one\nthree\n"},
		{"CRLF file", "crlf.txt", crlf("one\nthree\n"), "one\r\nthree\r\n"},
		{"leading blank line", "docs/guide.md", crlf("\nguide\n"), "\nguide\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := seedEditRepo(t)
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/"+tt.path,
				url.Values{"path": {tt.path}, "content": {tt.typed}, "blob_sha": {r.blobSHA(t, tt.path)}}, false)
			if want := r.path + "/blob/main/" + tt.path; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
				t.Fatalf("want 303 to %s, got %d %q: %.300s", want, rr.Code, rr.Header().Get("Location"), rr.Body.String())
			}
			if got := r.contents(t, tt.path); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.path, got, tt.want)
			}
			if got, want := r.headMessage(t), "Update "+tt.path; got != want {
				t.Errorf("message = %q, want %q", got, want)
			}
		})
	}
}

func TestEditFile_Renames(t *testing.T) {
	tests := []struct{ name, from, to, content, message, want string }{
		{"rename", "run.sh", "bin/run.sh", "echo\n", "", "Rename run.sh to bin/run.sh"},
		{"rename and edit", "docs/guide.md", "guide.md", "guide v2\n", "", "Update and rename docs/guide.md to guide.md"},
		{"custom message", "a.txt", "b.txt", "one\ntwo\n", "Move it", "Move it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := seedEditRepo(t)
			from, _ := r.file(t, "main", tt.from)
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/"+tt.from,
				url.Values{"path": {tt.to}, "content": {crlf(tt.content)}, "message": {tt.message}, "blob_sha": {from.Hash.String()}}, false)
			if want := r.path + "/blob/main/" + tt.to; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
				t.Fatalf("want 303 to %s, got %d %q: %.300s", want, rr.Code, rr.Header().Get("Location"), rr.Body.String())
			}
			to, ok := r.file(t, "main", tt.to)
			if !ok {
				t.Fatalf("%s is not on main", tt.to)
			}
			if to.Mode != from.Mode {
				t.Errorf("%s mode = %s, want %s", tt.to, to.Mode, from.Mode)
			}
			if got := r.contents(t, tt.to); got != tt.content {
				t.Errorf("%s = %q, want %q", tt.to, got, tt.content)
			}
			if _, ok := r.file(t, "main", tt.from); ok {
				t.Errorf("%s is still on main", tt.from)
			}
			if got := r.headMessage(t); got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEditFile_RefusalsKeepTheForm(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		status int
		want   string
		setup  func(t *testing.T, r editRepo) url.Values
	}{
		{"file changed", "a.txt", http.StatusConflict, "changed on main after you opened it", func(t *testing.T, r editRepo) url.Values {
			sha := r.blobSHA(t, "a.txt")
			r.commitMain(t, "a.txt", "theirs\n")
			return url.Values{"path": {"a.txt"}, "content": {"mine\r\n"}, "message": {"My edit"}, "blob_sha": {sha}}
		}},
		{"rename onto a file", "a.txt", http.StatusConflict, "m.txt is a file", func(t *testing.T, r editRepo) url.Values {
			return url.Values{"path": {"m.txt"}, "content": {"mine\r\n"}, "message": {"My edit"}, "blob_sha": {r.blobSHA(t, "a.txt")}}
		}},
		{"invalid path", "a.txt", http.StatusUnprocessableEntity, "reserved for Git", func(t *testing.T, r editRepo) url.Values {
			return url.Values{"path": {".git/config"}, "content": {"mine\r\n"}, "message": {"My edit"}, "blob_sha": {r.blobSHA(t, "a.txt")}}
		}},
		{"unchanged", "a.txt", http.StatusUnprocessableEntity, "Nothing to commit", func(t *testing.T, r editRepo) url.Values {
			return url.Values{"path": {"a.txt"}, "content": {"one\r\ntwo\r\n"}, "message": {"My edit"}, "blob_sha": {r.blobSHA(t, "a.txt")}}
		}},
		{"too large", "a.txt", http.StatusRequestEntityTooLarge, "files over 1 MB", func(t *testing.T, r editRepo) url.Values {
			return url.Values{"path": {"a.txt"}, "content": {strings.Repeat("a", editCap+1)}, "message": {"My edit"}, "blob_sha": {r.blobSHA(t, "a.txt")}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := seedEditRepo(t)
			form := tt.setup(t, r)
			tip := branchHash(t, r.git, "main")
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/"+tt.path, form, false)
			if rr.Code != tt.status {
				t.Fatalf("want %d, got %d: %.300s", tt.status, rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Errorf("body lacks %q", tt.want)
			}
			if tt.status != http.StatusRequestEntityTooLarge {
				content := strings.ReplaceAll(form.Get("content"), "\r\n", "\n")
				for _, keep := range []string{">\n" + content + "</textarea>", `value="` + form.Get("path") + `"`, `value="My edit"`} {
					if !strings.Contains(body, keep) {
						t.Errorf("re-rendered form lacks %q", keep)
					}
				}
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestEditFile_LandsAfterAnotherFileChanged(t *testing.T) {
	r := seedEditRepo(t)
	sha := r.blobSHA(t, "a.txt")
	r.commitMain(t, "m.txt", "theirs\n")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/a.txt",
		url.Values{"path": {"a.txt"}, "content": {"mine\r\n"}, "blob_sha": {sha}}, false)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %.300s", rr.Code, rr.Body.String())
	}
	if got := r.contents(t, "m.txt"); got != "theirs\n" {
		t.Errorf("m.txt = %q, want theirs", got)
	}
}

func TestEditFile_RefusesFilesTheEditorCantOpen(t *testing.T) {
	for _, tt := range []struct{ path, want string }{
		{"bin.dat", "binary files can't be edited in the browser\n"},
		{"link", "symlinks can't be edited in the browser\n"},
		{"big.txt", "files over 1 MB can't be edited in the browser\n"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			r := seedEditRepo(t)
			tip := branchHash(t, r.git, "main")
			if rr := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/edit/main/"+tt.path, nil, false); rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != tt.want {
				t.Errorf("GET: want 422 %q, got %d %.100q", tt.want, rr.Code, rr.Body.String())
			}
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/"+tt.path,
				url.Values{"path": {tt.path}, "content": {"x"}, "blob_sha": {r.blobSHA(t, tt.path)}}, false)
			if rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != tt.want {
				t.Errorf("POST: want 422 %q, got %d %.100q", tt.want, rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestEditAndDelete_RefuseWhoAndWhereTheyMust(t *testing.T) {
	r := seedEditRepo(t)
	reader := seedSignedInUser(t, r.db)
	sha := r.blobSHA(t, "a.txt")
	tip := branchHash(t, r.git, "main")
	tests := []struct {
		name, token, ref string
		archived         bool
		status           int
	}{
		{"anonymous", "", "main", false, http.StatusUnauthorized},
		{"reader", reader.token, "main", false, http.StatusForbidden},
		{"archived", r.owner.token, "main", true, http.StatusForbidden},
		{"tag", r.owner.token, "v1", false, http.StatusNotFound},
		{"commit SHA", r.owner.token, tip.String(), false, http.StatusNotFound},
		{"missing branch", r.owner.token, "gone", false, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Exec(t, r.db, `UPDATE repositories SET is_archived = $1 WHERE id = $2`, tt.archived, r.id)
			t.Cleanup(func() { testutil.Exec(t, r.db, `UPDATE repositories SET is_archived = false WHERE id = $1`, r.id) })
			form := url.Values{"path": {"a.txt"}, "content": {"mine\r\n"}, "blob_sha": {sha}}
			for _, req := range []struct {
				method, kind string
				form         url.Values
			}{
				{http.MethodGet, "edit", nil},
				{http.MethodPost, "edit", form},
				{http.MethodPost, "delete", url.Values{"blob_sha": {sha}}},
			} {
				rr := send(t, r.api, req.method, tt.token, r.path+"/"+req.kind+"/"+tt.ref+"/a.txt", req.form, false)
				if tt.status == http.StatusUnauthorized {
					if rr.Code < 300 {
						t.Errorf("%s %s: want a refusal, got %d", req.method, req.kind, rr.Code)
					}
				} else if rr.Code != tt.status {
					t.Errorf("%s %s: want %d, got %d: %.200s", req.method, req.kind, tt.status, rr.Code, rr.Body.String())
				}
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestEditFile_AuthorEmailFollowsKeepEmailPrivate(t *testing.T) {
	r := seedEditRepo(t)
	authorEmail := func(content string) string {
		t.Helper()
		rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/a.txt",
			url.Values{"path": {"a.txt"}, "content": {content}, "blob_sha": {r.blobSHA(t, "a.txt")}}, false)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("want 303, got %d: %.300s", rr.Code, rr.Body.String())
		}
		c, err := r.git.CommitObject(branchHash(t, r.git, "main"))
		if err != nil {
			t.Fatalf("load main: %v", err)
		}
		if c.Author.Name != r.owner.name {
			t.Errorf("author name = %q, want %q", c.Author.Name, r.owner.name)
		}
		return c.Author.Email
	}

	if got, want := authorEmail("mine\r\n"), fmt.Sprintf("%d+%s@users.noreply.localhost", r.owner.id, r.owner.name); got != want {
		t.Errorf("setting on: author email = %q, want %q", got, want)
	}
	saveEmailSettings(t, r.api, r.owner.token, url.Values{})
	if got, want := authorEmail("mine again\r\n"), r.owner.name+"@test.invalid"; got != want {
		t.Errorf("setting off: author email = %q, want %q", got, want)
	}
}

func TestDeleteFile_CommitsTheRemoval(t *testing.T) {
	tests := []struct{ name, path, message, wantMessage, wantTo string }{
		{"folder's only file", "docs/guide.md", "", "Delete docs/guide.md", "/tree/main"},
		{"binary file", "bin.dat", "Drop the blob", "Drop the blob", "/tree/main"},
		{"large file", "big.txt", "", "Delete big.txt", "/tree/main"},
		{"symlink", "link", "", "Delete link", "/tree/main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := seedEditRepo(t)
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/delete/main/"+tt.path,
				url.Values{"blob_sha": {r.blobSHA(t, tt.path)}, "message": {tt.message}}, true)
			if want := r.path + tt.wantTo; rr.Code != http.StatusNoContent || rr.Header().Get("HX-Redirect") != want {
				t.Fatalf("want 204 HX-Redirect %s, got %d %q: %.300s", want, rr.Code, rr.Header().Get("HX-Redirect"), rr.Body.String())
			}
			if _, ok := r.file(t, "main", tt.path); ok {
				t.Errorf("%s is still on main", tt.path)
			}
			if got := r.headMessage(t); got != tt.wantMessage {
				t.Errorf("message = %q, want %q", got, tt.wantMessage)
			}
		})
	}
}

func TestDeleteFile_LandsOnTheNearestRemainingFolder(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "docs/api.md", "api\n")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/delete/main/docs/guide.md",
		url.Values{"blob_sha": {r.blobSHA(t, "docs/guide.md")}}, false)
	if want := r.path + "/tree/main/docs"; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
		t.Fatalf("want 303 to %s, got %d %q", want, rr.Code, rr.Header().Get("Location"))
	}
}

func TestDeleteFile_RefusesAStaleFileAsJSON(t *testing.T) {
	r := seedEditRepo(t)
	sha := r.blobSHA(t, "a.txt")
	r.commitMain(t, "a.txt", "theirs\n")
	tip := branchHash(t, r.git, "main")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/delete/main/a.txt", url.Values{"blob_sha": {sha}}, true)
	var body struct{ Error string }
	if rr.Code != http.StatusConflict || json.Unmarshal(rr.Body.Bytes(), &body) != nil || !strings.Contains(body.Error, "changed on main") {
		t.Errorf("want 409 JSON error, got %d %.200q", rr.Code, rr.Body.String())
	}
	if got := branchHash(t, r.git, "main"); got != tip {
		t.Errorf("main = %s, want it left at %s", got, tip)
	}
}

func TestBlobPage_ShowsEditAndDeleteWhereAccepted(t *testing.T) {
	r := seedEditRepo(t)
	reader := seedSignedInUser(t, r.db)
	tip := branchHash(t, r.git, "main").String()
	tests := []struct {
		name, token, ref, path string
		archived               bool
		edit, del              bool
	}{
		{"writer, text file", r.owner.token, "main", "a.txt", false, true, true},
		{"writer, binary file", r.owner.token, "main", "bin.dat", false, false, true},
		{"writer, large file", r.owner.token, "main", "big.txt", false, false, true},
		{"writer, symlink", r.owner.token, "main", "link", false, false, true},
		{"writer, tag", r.owner.token, "v1", "a.txt", false, false, false},
		{"writer, commit SHA", r.owner.token, tip, "a.txt", false, false, false},
		{"writer, archived", r.owner.token, "main", "a.txt", true, false, false},
		{"reader", reader.token, "main", "a.txt", false, false, false},
		{"anonymous", "", "main", "a.txt", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Exec(t, r.db, `UPDATE repositories SET is_archived = $1 WHERE id = $2`, tt.archived, r.id)
			t.Cleanup(func() { testutil.Exec(t, r.db, `UPDATE repositories SET is_archived = false WHERE id = $1`, r.id) })
			rr := send(t, r.api, http.MethodGet, tt.token, r.path+"/blob/"+tt.ref+"/"+tt.path, nil, false)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rr.Code)
			}
			body := rr.Body.String()
			if got := strings.Contains(body, `href="`+r.path+"/edit/"+tt.ref+"/"+tt.path+`"`); got != tt.edit {
				t.Errorf("Edit shown = %v, want %v", got, tt.edit)
			}
			if got := strings.Contains(body, `hx-post="`+r.path+"/delete/"+tt.ref+"/"+tt.path+`"`); got != tt.del {
				t.Errorf("Delete shown = %v, want %v", got, tt.del)
			}
			if tt.del && !strings.Contains(body, `name="blob_sha" value="`+r.blobSHA(t, tt.path)+`"`) {
				t.Error("delete dialog lacks the blob SHA")
			}
		})
	}
}

func TestSubmitNewFile_TypedContentCommitsLF(t *testing.T) {
	r := seedEditRepo(t)
	if rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/new/main", url.Values{"path": {"typed.sh"}, "content": {"a\r\nb\r\n"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("typed: want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := r.contents(t, "typed.sh"); got != "a\nb\n" {
		t.Errorf("typed.sh = %q, want LF", got)
	}
	if rr := postUpload(t, r.api, r.owner.token, r.path+"/new/main", url.Values{"path": {"up.txt"}}, []byte("a\r\nb\r\n")); rr.Code != http.StatusSeeOther {
		t.Fatalf("upload: want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := r.contents(t, "up.txt"); got != "a\r\nb\r\n" {
		t.Errorf("up.txt = %q, want it byte for byte", got)
	}
}

func TestEditFile_AcceptsTheBrowserMultipartForm(t *testing.T) {
	r := seedEditRepo(t)
	contentType, body := uploadForm(t, url.Values{"path": {"a.txt"}, "content": {"x\r\n"}, "blob_sha": {r.blobSHA(t, "a.txt")}}, nil)
	req := httptest.NewRequest(http.MethodPost, r.path+"/edit/main/a.txt", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+r.owner.token)
	rr := httptest.NewRecorder()
	r.api.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %.300s", rr.Code, rr.Body.String())
	}
	if got := r.contents(t, "a.txt"); got != "x\n" {
		t.Errorf("a.txt = %q, want x\\n", got)
	}
}

func TestEditAndDelete_HideAPrivateRepo(t *testing.T) {
	r := seedEditRepo(t)
	stranger := seedSignedInUser(t, r.db)
	testutil.Exec(t, r.db, `UPDATE repositories SET private = true WHERE id = $1`, r.id)
	sha := r.blobSHA(t, "a.txt")
	for _, req := range []struct {
		method, kind string
		form         url.Values
	}{
		{http.MethodGet, "edit", nil},
		{http.MethodPost, "edit", url.Values{"path": {"a.txt"}, "content": {"x"}, "blob_sha": {sha}}},
		{http.MethodPost, "delete", url.Values{"blob_sha": {sha}}},
	} {
		if rr := send(t, r.api, req.method, stranger.token, r.path+"/"+req.kind+"/main/a.txt", req.form, false); rr.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404, got %d", req.method, req.kind, rr.Code)
		}
	}
}

func TestEditFile_RefusesTextATextareaWouldMangle(t *testing.T) {
	for name, content := range map[string]string{"latin1.txt": "caf\xe9\n", "cr.txt": "a\rb\n"} {
		t.Run(name, func(t *testing.T) {
			r := seedEditRepo(t)
			r.commitMain(t, name, content)
			if rr := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/edit/main/"+name, nil, false); rr.Code != http.StatusUnprocessableEntity {
				t.Errorf("GET: want 422, got %d", rr.Code)
			}
			body := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/blob/main/"+name, nil, false).Body.String()
			if strings.Contains(body, `href="`+r.path+"/edit/main/"+name+`"`) {
				t.Error("blob page shows Edit")
			}
		})
	}
}

func TestEditFile_MixedLineEndingsComeBackLF(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "mixed.txt", "one\r\ntwo\n")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/mixed.txt",
		url.Values{"path": {"mixed.txt"}, "content": {"one\r\ntwo\r\nthree\r\n"}, "blob_sha": {r.blobSHA(t, "mixed.txt")}}, false)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", rr.Code)
	}
	if got := r.contents(t, "mixed.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("mixed.txt = %q, want all LF", got)
	}
}

func TestEditFile_RefusesAnOversizedBodyUpFront(t *testing.T) {
	r := seedEditRepo(t)
	big := url.Values{"path": {"a.txt"}, "content": {strings.Repeat("a", 4<<20)}, "blob_sha": {r.blobSHA(t, "a.txt")}}
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/a.txt", big, false)
	if rr.Code != http.StatusRequestEntityTooLarge || len(rr.Body.String()) > 200 {
		t.Errorf("want a short 413, got %d with %d bytes", rr.Code, rr.Body.Len())
	}
}

func TestDeleteFile_LandsAfterAnotherFileChanged(t *testing.T) {
	r := seedEditRepo(t)
	sha := r.blobSHA(t, "a.txt")
	r.commitMain(t, "m.txt", "theirs\n")
	if rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/delete/main/a.txt", url.Values{"blob_sha": {sha}}, true); rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, ok := r.file(t, "main", "a.txt"); ok {
		t.Error("a.txt is still on main")
	}
}

func TestDeleteFile_RefusesFoldersAndStaleSHAs(t *testing.T) {
	r := seedEditRepo(t)
	tip := branchHash(t, r.git, "main")
	tree, err := r.git.CommitObject(tip)
	if err != nil {
		t.Fatalf("load main: %v", err)
	}
	root, _ := tree.Tree()
	docs, err := root.FindEntry("docs")
	if err != nil {
		t.Fatalf("find docs: %v", err)
	}
	for _, tt := range []struct {
		name, path, sha string
		status          int
	}{
		{"folder", "docs", docs.Hash.String(), http.StatusNotFound},
		{"stale SHA", "a.txt", strings.Repeat("0", 40), http.StatusConflict},
	} {
		if rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/delete/main/"+tt.path, url.Values{"blob_sha": {tt.sha}}, true); rr.Code != tt.status {
			t.Errorf("%s: want %d, got %d: %s", tt.name, tt.status, rr.Code, rr.Body.String())
		}
	}
	if got := branchHash(t, r.git, "main"); got != tip {
		t.Errorf("main = %s, want it left at %s", got, tip)
	}
}
