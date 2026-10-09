package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newCollisionTestRepo adds one entry of each kind to main, beside a.txt and m.txt.
func newCollisionTestRepo(t *testing.T) *tipTestRepo {
	t.Helper()
	r := newTipTestRepo(t)
	r.commit(t, "main", "docs/guide.md", "guide\n")
	r.commit(t, "main", "src/lib/x.go", "x\n")
	r.commit(t, "main", "src/main.go", "main\n")
	r.commitEntry(t, "main", "run.sh", filemode.Executable, r.blob(t, "echo\n"))
	r.commitEntry(t, "main", "link", filemode.Symlink, r.blob(t, "a.txt"))
	r.commitEntry(t, "main", "deps/lib", filemode.Submodule, mainLib)
	return r
}

func TestCommitFile_RefusesPathCollisions(t *testing.T) {
	tests := []struct {
		name, path, content string
		want                string
	}{
		{"file over a directory", "docs", "docs\n", "path collides with an existing entry: docs is a directory"},
		{"file over a nested directory", "src/lib", "lib\n", "path collides with an existing entry: src/lib is a directory"},
		{"file over a submodule", "deps/lib", "lib\n", "path collides with an existing entry: deps/lib is a submodule"},
		{"file over a symlink", "link", "link\n", "path collides with an existing entry: link is a symlink"},
		{"symlink's own target over it", "link", "a.txt", "path collides with an existing entry: link is a symlink"},
		{"directory over a file", "a.txt/x", "x\n", "path collides with an existing entry: a.txt is a file"},
		{"directory over a nested file", "src/main.go/x", "x\n", "path collides with an existing entry: src/main.go is a file"},
		{"directory over an executable", "run.sh/x", "x\n", "path collides with an existing entry: run.sh is a file"},
		{"directory over a symlink", "link/x", "x\n", "path collides with an existing entry: link is a symlink"},
		{"directory over a submodule", "deps/lib/x", "x\n", "path collides with an existing entry: deps/lib is a submodule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			tip := branchTip(t, r.repo, "main")
			_, err := r.svc.CommitFile(r.owner, r.name, "main", tt.path, []byte(tt.content), tipTestAuthor, "Add "+tt.path)
			if !errors.Is(err, ErrPathCollision) || err.Error() != tt.want {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
			if got := branchTip(t, r.repo, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestCommitFile_UpdatesFiles(t *testing.T) {
	tests := []struct {
		name, path, content string
		mode                filemode.FileMode
	}{
		{"replace a file", "a.txt", "new\n", filemode.Regular},
		{"replace an executable", "run.sh", "echo new\n", filemode.Executable},
		{"add a file to a directory", "docs/api.md", "api\n", filemode.Regular},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			if _, err := r.svc.CommitFile(r.owner, r.name, "main", tt.path, []byte(tt.content), tipTestAuthor, "Edit "+tt.path); err != nil {
				t.Fatalf("CommitFile: %v", err)
			}
			r.expectEntries(t, "main", map[string]mergeFile{
				tt.path:         {hash: r.blob(t, tt.content), mode: tt.mode},
				"docs/guide.md": {hash: r.blob(t, "guide\n"), mode: filemode.Regular},
			})
		})
	}
}

func TestCommitFile_RefusesUnchangedFiles(t *testing.T) {
	tests := []struct{ name, path, content string }{
		{"regular file", "a.txt", "a\n"},
		{"executable", "run.sh", "echo\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			tip := branchTip(t, r.repo, "main")
			_, err := r.svc.CommitFile(r.owner, r.name, "main", tt.path, []byte(tt.content), tipTestAuthor, "Edit "+tt.path)
			if !errors.Is(err, ErrFileUnchanged) {
				t.Errorf("err = %v, want file is unchanged", err)
			}
			if got := branchTip(t, r.repo, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

// deepPath spells an n-byte path in one-letter directories: the deepest path
// n bytes allow.
func deepPath(n int) string {
	dirs := (n - 1) / 2
	return strings.Repeat("d/", dirs) + strings.Repeat("f", n-2*dirs)
}

func TestCommitFile_CommitsTheDeepestPathUnderTheCap(t *testing.T) {
	r := newTipTestRepo(t)
	path := deepPath(maxFilePathBytes)
	r.commit(t, "main", path, "deep\n")
	r.expectEntries(t, "main", map[string]mergeFile{path: {hash: r.blob(t, "deep\n"), mode: filemode.Regular}})
}

func TestCommitFile_RefusesPathsOverTheCapUpFront(t *testing.T) {
	for _, n := range []int{maxFilePathBytes + 1, 1 << 20} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			r := newTipTestRepo(t)
			content := []byte("refused\n")
			done := make(chan error, 1)
			go func() {
				_, err := r.svc.CommitFile(r.owner, r.name, "main", deepPath(n), content, tipTestAuthor, "Add a deep file")
				done <- err
			}()
			select {
			case err := <-done:
				if !errors.Is(err, ErrFilePathTooLong) {
					t.Fatalf("err = %v, want ErrFilePathTooLong", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("CommitFile still busy after 10s")
			}
			if r.repo.Storer.HasEncodedObject(plumbing.ComputeHash(plumbing.BlobObject, content)) == nil {
				t.Error("the refused commit stored its blob")
			}
		})
	}
}

func TestCommitFile_RefusesInvalidPaths(t *testing.T) {
	paths := []string{
		"/",
		"a//b",
		"a/./b",
		"../b",
		".git/hooks/post-checkout",
		".GIT/config",
		"src/.Git/config",
		"docs/.git",
		"git~1/config",
		"GIT~1/config",
		".git./config",
		".git . /config",
		".git::$INDEX_ALLOCATION/config",
		"git~1:stream/config",
		fmt.Sprintf(".g%cit/config", 0x200c),
		fmt.Sprintf("%c.GIT%c/config", 0xfeff, 0x200e),
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			r := newTipTestRepo(t)
			_, err := r.svc.CommitFile(r.owner, r.name, "main", p, []byte("hook\n"), tipTestAuthor, "Add "+p)
			if !errors.Is(err, ErrInvalidFilePath) {
				t.Errorf("err = %v, want ErrInvalidFilePath", err)
			}
			if got := branchTip(t, r.repo, "main"); got != r.mainTip {
				t.Errorf("main = %s, want %s", got, r.mainTip)
			}
		})
	}
}

func TestCommitFile_AcceptsNamesThatOnlyStartLikeDotGit(t *testing.T) {
	r := newTipTestRepo(t)
	want := map[string]mergeFile{}
	for _, p := range []string{".gitignore", ".gitmodules", ".github/workflows/ci.yml", ".git-blame-ignore-revs", "vendor/lib.git/HEAD", "git~2/notes", "git/config"} {
		r.commit(t, "main", p, p+"\n")
		want[p] = mergeFile{hash: r.blob(t, p+"\n"), mode: filemode.Regular}
	}
	r.expectEntries(t, "main", want)
}

func TestEditFile_EditsInPlace(t *testing.T) {
	tests := []struct {
		name, path, content string
		mode                filemode.FileMode
	}{
		{"regular file", "a.txt", "new\n", filemode.Regular},
		{"executable", "run.sh", "echo new\n", filemode.Executable},
		{"nested file", "docs/guide.md", "guide v2\n", filemode.Regular},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			base := r.entryHash(t, "main", tt.path)
			if _, err := r.svc.EditFile(r.owner, r.name, "main", tt.path, tt.path, base.String(), []byte(tt.content), tipTestAuthor, "Update "+tt.path); err != nil {
				t.Fatalf("EditFile: %v", err)
			}
			r.expectEntries(t, "main", map[string]mergeFile{
				tt.path: {hash: r.blob(t, tt.content), mode: tt.mode},
				"m.txt": {hash: r.blob(t, "m\n"), mode: filemode.Regular},
			})
		})
	}
}

func TestEditFile_Renames(t *testing.T) {
	tests := []struct {
		name, from, to, content string
		mode                    filemode.FileMode
		gone                    []string
	}{
		{"within a folder", "src/main.go", "src/app.go", "main\n", filemode.Regular, []string{"src/main.go"}},
		{"into new folders", "a.txt", "notes/2026/a.txt", "a\n", filemode.Regular, []string{"a.txt"}},
		{"out of a folder it empties", "docs/guide.md", "guide.md", "guide\n", filemode.Regular, []string{"docs"}},
		{"an executable, with new content", "run.sh", "bin/run.sh", "echo moved\n", filemode.Executable, []string{"run.sh"}},
		{"onto the folder it empties", "docs/guide.md", "docs", "guide\n", filemode.Regular, []string{"docs/guide.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			base := r.entryHash(t, "main", tt.from)
			if _, err := r.svc.EditFile(r.owner, r.name, "main", tt.from, tt.to, base.String(), []byte(tt.content), tipTestAuthor, "Rename"); err != nil {
				t.Fatalf("EditFile: %v", err)
			}
			r.expectEntries(t, "main", map[string]mergeFile{
				tt.to:          {hash: r.blob(t, tt.content), mode: tt.mode},
				"src/lib/x.go": {hash: r.blob(t, "x\n"), mode: filemode.Regular},
			})
			tree := r.tipTree(t, "main")
			for _, p := range tt.gone {
				if _, err := tree.FindEntry(p); err == nil {
					t.Errorf("%s is still on main", p)
				}
			}
		})
	}
}

func TestEditFile_RefusesARenameOntoAnExistingEntry(t *testing.T) {
	for _, to := range []string{"m.txt", "docs", "link", "deps/lib", "src/lib/x.go", "a.txt/x"} {
		t.Run(to, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			tip := branchTip(t, r.repo, "main")
			base := r.entryHash(t, "main", "src/main.go")
			_, err := r.svc.EditFile(r.owner, r.name, "main", "src/main.go", to, base.String(), []byte("main\n"), tipTestAuthor, "Rename")
			if !errors.Is(err, ErrPathCollision) {
				t.Errorf("err = %v, want ErrPathCollision", err)
			}
			if got := branchTip(t, r.repo, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestEditFile_RefusesAStaleBase(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, r *tipTestRepo)
	}{
		{"file edited", func(t *testing.T, r *tipTestRepo) { r.commit(t, "main", "a.txt", "theirs\n") }},
		{"file replaced by a folder", func(t *testing.T, r *tipTestRepo) {
			r.removeEntry(t, "main", "a.txt")
			r.commit(t, "main", "a.txt/x", "x\n")
		}},
		{"file deleted", func(t *testing.T, r *tipTestRepo) { r.removeEntry(t, "main", "a.txt") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, to := range []string{"a.txt", "b.txt"} {
				r := newCollisionTestRepo(t)
				base := r.entryHash(t, "main", "a.txt")
				tt.change(t, r)
				tip := branchTip(t, r.repo, "main")
				_, err := r.svc.EditFile(r.owner, r.name, "main", "a.txt", to, base.String(), []byte("mine\n"), tipTestAuthor, "Update")
				if !errors.Is(err, ErrFileChanged) {
					t.Errorf("to %s: err = %v, want ErrFileChanged", to, err)
				}
				if got := branchTip(t, r.repo, "main"); got != tip {
					t.Errorf("to %s: main = %s, want it left at %s", to, got, tip)
				}
			}
		})
	}
}

func TestEditFile_LandsOnATipThatChangedOtherFiles(t *testing.T) {
	r := newCollisionTestRepo(t)
	base := r.entryHash(t, "main", "a.txt")
	r.commit(t, "main", "m.txt", "theirs\n")
	if _, err := r.svc.EditFile(r.owner, r.name, "main", "a.txt", "a.txt", base.String(), []byte("mine\n"), tipTestAuthor, "Update"); err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	r.expectEntries(t, "main", map[string]mergeFile{
		"a.txt": {hash: r.blob(t, "mine\n"), mode: filemode.Regular},
		"m.txt": {hash: r.blob(t, "theirs\n"), mode: filemode.Regular},
	})
}

func TestEditFile_RefusesAnUnchangedFile(t *testing.T) {
	r := newCollisionTestRepo(t)
	tip := branchTip(t, r.repo, "main")
	base := r.entryHash(t, "main", "run.sh")
	_, err := r.svc.EditFile(r.owner, r.name, "main", "run.sh", "/run.sh", base.String(), []byte("echo\n"), tipTestAuthor, "Update")
	if !errors.Is(err, ErrFileUnchanged) {
		t.Errorf("err = %v, want ErrFileUnchanged", err)
	}
	if got := branchTip(t, r.repo, "main"); got != tip {
		t.Errorf("main = %s, want it left at %s", got, tip)
	}
}

func TestEditFile_LeavesAnUncleanPathItKeeps(t *testing.T) {
	r := newCollisionTestRepo(t)
	r.commitEntry(t, "main", `win\name.txt`, filemode.Regular, r.blob(t, "w\n"))
	base := r.entryHash(t, "main", `win\name.txt`)
	if _, err := r.svc.EditFile(r.owner, r.name, "main", `win\name.txt`, `win\name.txt`, base.String(), []byte("w2\n"), tipTestAuthor, "Update"); err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	r.expectEntries(t, "main", map[string]mergeFile{`win\name.txt`: {hash: r.blob(t, "w2\n"), mode: filemode.Regular}})
}

func TestIsEditableText(t *testing.T) {
	for s, want := range map[string]bool{
		"":              true,
		"a\nb\n":        true,
		"a\r\nb\r\n":    true,
		"café\n":        true,
		"caf\xe9\n":     false,
		"a\rb\n":        false,
		"a\r\nb\rc\r\n": false,
	} {
		if got := IsEditableText(s); got != want {
			t.Errorf("IsEditableText(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestEditFile_RefusesInvalidTargets(t *testing.T) {
	for _, to := range []string{"", "a/../b", ".git/config", deepPath(maxFilePathBytes + 1)} {
		t.Run(to, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			base := r.entryHash(t, "main", "a.txt")
			_, err := r.svc.EditFile(r.owner, r.name, "main", "a.txt", to, base.String(), []byte("a\n"), tipTestAuthor, "Rename")
			if !errors.Is(err, ErrInvalidFilePath) {
				t.Errorf("err = %v, want ErrInvalidFilePath", err)
			}
		})
	}
}

func TestEditFile_NeverCreatesABranch(t *testing.T) {
	r := newCollisionTestRepo(t)
	base := r.entryHash(t, "main", "a.txt")
	_, err := r.svc.EditFile(r.owner, r.name, "gone", "a.txt", "a.txt", base.String(), []byte("new\n"), tipTestAuthor, "Update")
	if !errors.Is(err, ErrRefNotFound) {
		t.Errorf("err = %v, want ErrRefNotFound", err)
	}
	if _, err := r.repo.Reference(plumbing.NewBranchReferenceName("gone"), true); err == nil {
		t.Error("EditFile created the branch")
	}
}

func TestDeleteFile_RemovesEntriesAndPrunesFolders(t *testing.T) {
	tests := []struct{ name, path, wantDir string }{
		{"root file", "a.txt", ""},
		{"symlink", "link", ""},
		{"folder's only file", "docs/guide.md", ""},
		{"file beside a folder", "src/main.go", "src"},
		{"nested folder's only file", "src/lib/x.go", "src"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			base := r.entryHash(t, "main", tt.path)
			dir, _, err := r.svc.DeleteFile(r.owner, r.name, "main", tt.path, base.String(), tipTestAuthor, "Delete "+tt.path)
			if err != nil {
				t.Fatalf("DeleteFile: %v", err)
			}
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			tree := r.tipTree(t, "main")
			if _, err := tree.FindEntry(tt.path); err == nil {
				t.Errorf("%s is still on main", tt.path)
			}
			for _, empty := range []string{"docs", "src/lib"} {
				if e, err := tree.FindEntry(empty); err == nil {
					if sub, err := r.repo.TreeObject(e.Hash); err == nil && len(sub.Entries) == 0 {
						t.Errorf("%s is left as an empty tree", empty)
					}
				}
			}
			r.expectEntries(t, "main", map[string]mergeFile{"m.txt": {hash: r.blob(t, "m\n"), mode: filemode.Regular}})
		})
	}
}

func TestDeleteFile_DeletesTheLastFile(t *testing.T) {
	r := newTipTestRepo(t)
	for _, p := range []string{"a.txt", "m.txt"} {
		base := r.entryHash(t, "main", p)
		if _, _, err := r.svc.DeleteFile(r.owner, r.name, "main", p, base.String(), tipTestAuthor, "Delete "+p); err != nil {
			t.Fatalf("delete %s: %v", p, err)
		}
	}
	if n := len(r.tipTree(t, "main").Entries); n != 0 {
		t.Errorf("main's tree has %d entries, want 0", n)
	}
}

func TestDeleteFile_Refuses(t *testing.T) {
	tests := []struct {
		name, branch, path string
		base               func(r *tipTestRepo) string
		want               error
	}{
		{"stale base", "main", "a.txt", func(r *tipTestRepo) string { return r.blob(t, "old\n").String() }, ErrFileChanged},
		{"missing file", "main", "nope.txt", func(r *tipTestRepo) string { return r.blob(t, "a\n").String() }, ErrFileChanged},
		{"submodule", "main", "deps/lib", func(*tipTestRepo) string { return mainLib.String() }, object.ErrFileNotFound},
		{"folder", "main", "docs", func(r *tipTestRepo) string { return r.entryHash(t, "main", "docs").String() }, object.ErrFileNotFound},
		{"malformed base", "main", "a.txt", func(*tipTestRepo) string { return "abc" }, ErrFileChanged},
		{"missing branch", "gone", "a.txt", func(r *tipTestRepo) string { return r.blob(t, "a\n").String() }, ErrRefNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCollisionTestRepo(t)
			tip := branchTip(t, r.repo, "main")
			_, _, err := r.svc.DeleteFile(r.owner, r.name, tt.branch, tt.path, tt.base(r), tipTestAuthor, "Delete")
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
			if got := branchTip(t, r.repo, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestGetBranchFile(t *testing.T) {
	r := newCollisionTestRepo(t)
	r.commitEntry(t, "main", "bin.dat", filemode.Regular, r.blob(t, "a\x00b"))
	if err := r.svc.CreateTag(r.owner, r.name, "v1", "main"); err != nil {
		t.Fatalf("create tag: %v", err)
	}

	f, err := r.svc.GetBranchFile(r.owner, r.name, "main", "run.sh", 1<<20)
	if err != nil {
		t.Fatalf("run.sh: %v", err)
	}
	if f.SHA != r.blob(t, "echo\n").String() || f.Mode != filemode.Executable || string(f.Content) != "echo\n" || f.IsBinary {
		t.Errorf("run.sh = %+v", f)
	}
	if f, err := r.svc.GetBranchFile(r.owner, r.name, "main", "run.sh", 2); err != nil || f.Content != nil || f.Size != 5 {
		t.Errorf("run.sh over the cap = %+v, %v; want no content", f, err)
	}
	if f, err := r.svc.GetBranchFile(r.owner, r.name, "main", "bin.dat", 1<<20); err != nil || !f.IsBinary || f.Content != nil {
		t.Errorf("bin.dat = %+v, %v; want binary with no content", f, err)
	}
	if f, err := r.svc.GetBranchFile(r.owner, r.name, "main", "link", 1<<20); err != nil || f.Mode != filemode.Symlink || f.Content != nil {
		t.Errorf("link = %+v, %v; want a symlink with no content", f, err)
	}
	for _, p := range []string{"docs", "deps/lib", "nope"} {
		if _, err := r.svc.GetBranchFile(r.owner, r.name, "main", p, 1<<20); !errors.Is(err, object.ErrFileNotFound) {
			t.Errorf("%s: err = %v, want ErrFileNotFound", p, err)
		}
	}
	for _, ref := range []string{"v1", branchTip(t, r.repo, "main").String(), "gone"} {
		if _, err := r.svc.GetBranchFile(r.owner, r.name, ref, "a.txt", 1<<20); !errors.Is(err, ErrRefNotFound) {
			t.Errorf("ref %s: err = %v, want ErrRefNotFound", ref, err)
		}
	}
}

func TestGetBlob_ReportsSHASymlinkAndBranch(t *testing.T) {
	r := newCollisionTestRepo(t)
	if err := r.svc.CreateTag(r.owner, r.name, "v1", "main"); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	b, err := r.svc.GetBlob(r.owner, r.name, "main", "link")
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if b.SHA != r.blob(t, "a.txt").String() || !b.IsSymlink || !b.IsBranch {
		t.Errorf("link on main = SHA %s symlink %v branch %v", b.SHA, b.IsSymlink, b.IsBranch)
	}
	for _, ref := range []string{"v1", branchTip(t, r.repo, "main").String()} {
		b, err := r.svc.GetBlob(r.owner, r.name, ref, "a.txt")
		if err != nil {
			t.Fatalf("GetBlob at %s: %v", ref, err)
		}
		if b.IsBranch || b.IsSymlink {
			t.Errorf("a.txt at %s = branch %v symlink %v, want neither", ref, b.IsBranch, b.IsSymlink)
		}
	}
}

// entryHash returns the hash of the entry at path on branch's tip.
func (r *tipTestRepo) entryHash(t *testing.T, branch, path string) plumbing.Hash {
	t.Helper()
	e, err := r.tipTree(t, branch).FindEntry(path)
	if err != nil {
		t.Fatalf("%s %s: %v", branch, path, err)
	}
	return e.Hash
}

// removeEntry commits branch's tip without path, leaving its folder even if empty.
func (r *tipTestRepo) removeEntry(t *testing.T, branch, path string) {
	t.Helper()
	tree := r.tipTree(t, branch)
	var entries []object.TreeEntry
	for _, e := range tree.Entries {
		if e.Name != path {
			entries = append(entries, e)
		}
	}
	parent := branchTip(t, r.repo, branch)
	sig := tipTestAuthor.signature(time.Now())
	tip := r.put(t, (&object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "Remove " + path,
		TreeHash:     r.put(t, (&object.Tree{Entries: entries}).Encode),
		ParentHashes: []plumbing.Hash{parent},
	}).Encode)
	setBranch(t, r.repo, branch, tip)
}
