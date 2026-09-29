package service

import (
	"errors"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var tipTestAuthor = GitAuthor{Name: "Tester", Email: "tester@example.com"}

// tipTestRepo is a bare repo with main and feature (branched from main's first
// commit). mainPushed and featurePushed are commits on top of each tip that no
// branch points at yet: the pushes a test lands.
type tipTestRepo struct {
	svc                       *CodeService
	root, owner, name, gitDir string
	repo                      *gogit.Repository
	mainTip, mainPushed       plumbing.Hash
	featureTip, featurePushed plumbing.Hash
}

func newTipTestRepo(t *testing.T) *tipTestRepo {
	t.Helper()
	r := &tipTestRepo{root: t.TempDir(), owner: "alice", name: "tips"}
	r.svc = NewCodeService(czconfig.GitConfig{ReposRoot: r.root})
	r.gitDir = filepath.Join(r.root, r.owner, r.name+".git")
	repo, err := gogit.PlainInit(r.gitDir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	r.repo = repo

	r.commit(t, "main", "a.txt", "a\n")
	if err := r.svc.CreateBranch(r.owner, r.name, "feature", "main"); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	r.featureTip = r.commit(t, "feature", "f.txt", "one\ntwo\n")
	r.featurePushed = pushedOnto(t, r.repo, "feature", func() { r.commit(t, "feature", "pushed.txt", "pushed\n") })
	r.mainTip = r.commit(t, "main", "m.txt", "m\n")
	r.mainPushed = pushedOnto(t, r.repo, "main", func() { r.commit(t, "main", "pushed.txt", "pushed\n") })
	return r
}

func (r *tipTestRepo) commit(t *testing.T, branch, path, content string) plumbing.Hash {
	t.Helper()
	if err := r.svc.CommitFile(r.owner, r.name, branch, path, []byte(content), tipTestAuthor, "Add "+path); err != nil {
		t.Fatalf("commit %s on %s: %v", path, branch, err)
	}
	return branchTip(t, r.repo, branch)
}

// pushedOnto runs commit, which must advance branch by one commit, then puts
// branch back where it was and returns the new commit.
func pushedOnto(t *testing.T, repo *gogit.Repository, branch string, commit func()) plumbing.Hash {
	t.Helper()
	tip := branchTip(t, repo, branch)
	commit()
	pushed := branchTip(t, repo, branch)
	setBranch(t, repo, branch, tip)
	return pushed
}

func branchTip(t *testing.T, repo *gogit.Repository, branch string) plumbing.Hash {
	t.Helper()
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatalf("resolve %s: %v", branch, err)
	}
	return ref.Hash()
}

func setBranch(t *testing.T, repo *gogit.Repository, branch string, h plumbing.Hash) {
	t.Helper()
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), h)); err != nil {
		t.Fatalf("set %s: %v", branch, err)
	}
}

func TestSetBranchTip_AdvancesAndCreates(t *testing.T) {
	r := newTipTestRepo(t)
	mainRef := plumbing.NewBranchReferenceName("main")
	if err := setBranchTip(r.repo.Storer, mainRef, r.mainTip, r.mainPushed); err != nil {
		t.Fatalf("advance main: %v", err)
	}
	if got := branchTip(t, r.repo, "main"); got != r.mainPushed {
		t.Errorf("main = %s, want %s", got, r.mainPushed)
	}

	if err := setBranchTip(r.repo.Storer, plumbing.NewBranchReferenceName("fresh"), plumbing.ZeroHash, r.mainTip); err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	if got := branchTip(t, r.repo, "fresh"); got != r.mainTip {
		t.Errorf("fresh = %s, want %s", got, r.mainTip)
	}
}

func TestSetBranchTip_BranchMovedSinceRead(t *testing.T) {
	r := newTipTestRepo(t)
	setBranch(t, r.repo, "main", r.mainPushed)

	err := setBranchTip(r.repo.Storer, plumbing.NewBranchReferenceName("main"), r.mainTip, r.featureTip)
	if !errors.Is(err, ErrRefMoved) {
		t.Errorf("err = %v, want ErrRefMoved", err)
	}
	if got := branchTip(t, r.repo, "main"); got != r.mainPushed {
		t.Errorf("main = %s, want pushed commit %s", got, r.mainPushed)
	}
}

func TestSetBranchTip_BranchCreatedSinceRead(t *testing.T) {
	r := newTipTestRepo(t)
	setBranch(t, r.repo, "fresh", r.mainPushed)

	err := setBranchTip(r.repo.Storer, plumbing.NewBranchReferenceName("fresh"), plumbing.ZeroHash, r.mainTip)
	if !errors.Is(err, ErrRefMoved) {
		t.Errorf("err = %v, want ErrRefMoved", err)
	}
	if got := branchTip(t, r.repo, "fresh"); got != r.mainPushed {
		t.Errorf("fresh = %s, want pushed commit %s", got, r.mainPushed)
	}
}

// go-git's CheckAndSetReference leaves an empty loose ref file behind when the
// ref is gone, and every ref listing then fails, so clone and fetch break.
func TestSetBranchTip_BranchDeletedSinceRead(t *testing.T) {
	r := newTipTestRepo(t)
	mainRef := plumbing.NewBranchReferenceName("main")
	if err := r.repo.Storer.RemoveReference(mainRef); err != nil {
		t.Fatalf("delete main: %v", err)
	}

	err := setBranchTip(r.repo.Storer, mainRef, r.mainTip, r.mainPushed)
	if !errors.Is(err, ErrRefMoved) {
		t.Errorf("err = %v, want ErrRefMoved", err)
	}
	refs, err := r.repo.References()
	if err != nil {
		t.Fatalf("list refs: %v", err)
	}
	if err := refs.ForEach(func(*plumbing.Reference) error { return nil }); err != nil {
		t.Errorf("list refs after failed update: %v", err)
	}
}

func TestWebCommits_PushLandsMidCommit(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		commit func(r *tipTestRepo) error
	}{
		{"apply suggestion", "feature", func(r *tipTestRepo) error {
			return r.svc.ApplySuggestion(r.owner, r.name, "feature", "f.txt", 1, "uno", tipTestAuthor)
		}},
		{"commit file", "main", func(r *tipTestRepo) error {
			return r.svc.CommitFile(r.owner, r.name, "main", "web.txt", []byte("web\n"), tipTestAuthor, "Add web.txt")
		}},
		{"three-way merge", "main", func(r *tipTestRepo) error {
			return r.svc.ThreeWayMergePullRequest(r.owner, r.name, "main", "feature", tipTestAuthor)
		}},
		{"squash merge", "main", func(r *tipTestRepo) error {
			return r.svc.SquashMergePullRequest(r.owner, r.name, "main", "feature", tipTestAuthor)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			from, pushed := r.mainTip, r.mainPushed
			if tt.branch == "feature" {
				from, pushed = r.featureTip, r.featurePushed
			}

			var err error
			testutil.PushDuringCommit(t, r.gitDir, plumbing.NewBranchReferenceName(tt.branch), from, pushed,
				func() { err = tt.commit(r) })

			if !errors.Is(err, ErrRefMoved) {
				t.Errorf("err = %v, want ErrRefMoved", err)
			}
			if got := branchTip(t, r.repo, tt.branch); got != pushed {
				t.Errorf("%s = %s, want pushed commit %s", tt.branch, got, pushed)
			}
		})
	}
}

func TestWikiEdits_PushLandsMidCommit(t *testing.T) {
	tests := []struct {
		name   string
		commit func(svc *CodeService) error
	}{
		{"save", func(svc *CodeService) error {
			return svc.WikiPageSave("alice", "tips", "web", "# Web", tipTestAuthor, "")
		}},
		{"rename", func(svc *CodeService) error {
			return svc.WikiPageRename("alice", "tips", "home", "start", tipTestAuthor, "")
		}},
		{"delete", func(svc *CodeService) error {
			return svc.WikiPageDelete("alice", "tips", "home", tipTestAuthor)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			if err := r.svc.WikiPageSave(r.owner, r.name, "home", "# Home", tipTestAuthor, ""); err != nil {
				t.Fatalf("seed wiki: %v", err)
			}
			wikiDir := filepath.Join(r.root, r.owner, r.name+".wiki.git")
			wiki, err := gogit.PlainOpen(wikiDir)
			if err != nil {
				t.Fatalf("open wiki: %v", err)
			}
			from := branchTip(t, wiki, "main")
			pushed := pushedOnto(t, wiki, "main", func() {
				if err := r.svc.WikiPageSave(r.owner, r.name, "pushed", "# Pushed", tipTestAuthor, ""); err != nil {
					t.Fatalf("pushed wiki commit: %v", err)
				}
			})

			testutil.PushDuringCommit(t, wikiDir, plumbing.NewBranchReferenceName("main"), from, pushed,
				func() { err = tt.commit(r.svc) })

			if !errors.Is(err, ErrRefMoved) {
				t.Errorf("err = %v, want ErrRefMoved", err)
			}
			if got := branchTip(t, wiki, "main"); got != pushed {
				t.Errorf("wiki main = %s, want pushed commit %s", got, pushed)
			}
		})
	}
}
