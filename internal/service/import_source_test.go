package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestParseImportURL(t *testing.T) {
	const prefix = "https://example.com/"
	atLimit := prefix + strings.Repeat("a", maxImportURLBytes-len(prefix))
	for _, tc := range []struct {
		in, want string
		err      error
	}{
		{"https://github.com/go-git/go-git.git", "https://github.com/go-git/go-git.git", nil},
		{"  http://git.example.com/a/b  ", "http://git.example.com/a/b", nil},
		{"https://github.com/a/b#readme", "https://github.com/a/b", nil},
		{"https://user:tok@github.com/a/b", "", ErrImportURLUserinfo},
		{"https://tok@github.com/a/b", "", ErrImportURLUserinfo},
		{"/srv/repos/alice/secret.git", "", ErrImportURL},
		{"file:///srv/repos/alice/secret.git", "", ErrImportURL},
		{"git@github.com:a/b.git", "", ErrImportURL},
		{"ssh://git@github.com/a/b.git", "", ErrImportURL},
		{"github.com/a/b", "", ErrImportURL},
		{"https:///a/b", "", ErrImportURL},
		{"", "", ErrImportURL},
		{"https://github.com/a/b.git?token=s3cret", "", ErrImportURL},
		{"https://github.com/a/b.git?", "", ErrImportURL},
		{"https://github.com/a/b?x=1#frag", "", ErrImportURL},
		{atLimit, atLimit, nil},
		{"  " + atLimit + "  ", atLimit, nil},
		{atLimit + "a", "", ErrImportURL},
	} {
		got, err := ParseImportURL(tc.in)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("ParseImportURL(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestImportAuth(t *testing.T) {
	if importAuth("", "") != nil {
		t.Error("importAuth without a token must be a nil interface")
	}
	if importAuth("alice", "tok") == nil {
		t.Error("importAuth with a token returned nil")
	}
}

func TestImportProgress_KeepsLastLine(t *testing.T) {
	var p importProgress
	_, _ = p.Write([]byte("Enumerating objects: 5, done.\nCounting objects:  50% (1/2)\r"))
	_, _ = p.Write([]byte("Counting objects: 100% (2/2), done.\r\nCompress"))
	if got := p.String(); got != "Counting objects: 100% (2/2), done." {
		t.Errorf("String() = %q", got)
	}
	_, _ = p.Write([]byte(strings.Repeat("x", 500) + "\n"))
	if got := p.String(); len(got) != maxProgressLine {
		t.Errorf("len(String()) = %d, want %d", len(got), maxProgressLine)
	}
}

func TestDefaultImportBranch(t *testing.T) {
	h := plumbing.NewHash("1111111111111111111111111111111111111111")
	branch := func(n string) *plumbing.Reference {
		return plumbing.NewHashReference(plumbing.NewBranchReferenceName(n), h)
	}
	head := func(n string) *plumbing.Reference {
		return plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(n))
	}
	tag := plumbing.NewHashReference(plumbing.NewTagReferenceName("v1"), h)
	for _, tc := range []struct {
		name string
		refs []*plumbing.Reference
		want string
		err  error
	}{
		{"HEAD target", []*plumbing.Reference{head("develop"), branch("main"), branch("develop")}, "develop", nil},
		{"HEAD on a missing branch falls back to main", []*plumbing.Reference{head("gone"), branch("zeta"), branch("main")}, "main", nil},
		{"no HEAD and no main: first by name", []*plumbing.Reference{branch("zeta"), branch("alpha")}, "alpha", nil},
		{"nested branch name", []*plumbing.Reference{head("release/1.0"), branch("release/1.0")}, "release/1.0", nil},
		{"tags only", []*plumbing.Reference{tag}, "", ErrImportEmptySource},
	} {
		got, err := defaultImportBranch(tc.refs)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}
