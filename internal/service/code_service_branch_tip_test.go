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
			return r.svc.ThreeWayMergePullRequest(r.owner, r.name, "main", "feature", r.featureTip, tipTestAuthor)
		}},
		{"squash merge", "main", func(r *tipTestRepo) error {
			return r.svc.SquashMergePullRequest(r.owner, r.name, "main", "feature", r.featureTip, tipTestAuthor)
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

func TestPullMerges_HeadMovedSinceCheck(t *testing.T) {
	tests := []struct {
		name  string
		merge func(r *tipTestRepo, checked plumbing.Hash) error
	}{
		{"fast-forward", func(r *tipTestRepo, checked plumbing.Hash) error {
			return r.svc.MergePullRequest(r.owner, r.name, "main", "feature", checked)
		}},
		{"three-way", func(r *tipTestRepo, checked plumbing.Hash) error {
			return r.svc.ThreeWayMergePullRequest(r.owner, r.name, "main", "feature", checked, tipTestAuthor)
		}},
		{"squash", func(r *tipTestRepo, checked plumbing.Hash) error {
			return r.svc.SquashMergePullRequest(r.owner, r.name, "main", "feature", checked, tipTestAuthor)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			setBranch(t, r.repo, "feature", r.featurePushed)

			if err := tt.merge(r, r.featureTip); !errors.Is(err, ErrRefMoved) {
				t.Errorf("err = %v, want ErrRefMoved", err)
			}
			if got := branchTip(t, r.repo, "main"); got != r.mainTip {
				t.Errorf("main = %s, want it left at %s", got, r.mainTip)
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
