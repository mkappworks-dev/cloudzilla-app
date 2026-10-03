package service

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/filemode"
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
			err := r.svc.CommitFile(r.owner, r.name, "main", tt.path, []byte(tt.content), tipTestAuthor, "Add "+tt.path)
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
			if err := r.svc.CommitFile(r.owner, r.name, "main", tt.path, []byte(tt.content), tipTestAuthor, "Edit "+tt.path); err != nil {
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
			err := r.svc.CommitFile(r.owner, r.name, "main", tt.path, []byte(tt.content), tipTestAuthor, "Edit "+tt.path)
			if err == nil || err.Error() != "file is unchanged" {
				t.Errorf("err = %v, want file is unchanged", err)
			}
			if got := branchTip(t, r.repo, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}
