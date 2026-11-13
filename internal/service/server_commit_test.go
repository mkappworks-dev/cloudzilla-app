package service

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
)

const serverCommitBaseURL = "https://git.example.test"

// serverCommitSite makes one server commit on r and returns the repository holding it and its hash.
type serverCommitSite struct {
	name string
	make func(t *testing.T, r *tipTestRepo) (dir string, hash plumbing.Hash)
}

func blobOf(t *testing.T, r *tipTestRepo, branch, path string) string {
	t.Helper()
	c, err := r.repo.CommitObject(branchTip(t, r.repo, branch))
	if err != nil {
		t.Fatal(err)
	}
	f, err := c.File(path)
	if err != nil {
		t.Fatal(err)
	}
	return f.Hash.String()
}

func wikiTip(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
	t.Helper()
	dir := filepath.Join(r.root, r.owner, r.name+".wiki.git")
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, branchTip(t, repo, "main")
}

var serverCommitSites = []serverCommitSite{
	{"three-way merge", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		if err := r.svc.ThreeWayMergePullRequest(r.owner, r.name, "main", "feature", r.featureTip, tipTestAuthor, ""); err != nil {
			t.Fatal(err)
		}
		return r.gitDir, branchTip(t, r.repo, "main")
	}},
	{"update branch", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		upd, err := r.svc.UpdatePullBranch(r.owner, r.name, "main", "feature", r.featureTip, tipTestAuthor)
		if err != nil {
			t.Fatal(err)
		}
		return r.gitDir, upd.New
	}},
	{"squash merge", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		if err := r.svc.SquashMergePullRequest(r.owner, r.name, "main", "feature", r.featureTip, tipTestAuthor, ""); err != nil {
			t.Fatal(err)
		}
		return r.gitDir, branchTip(t, r.repo, "main")
	}},
	{"apply suggestions", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		upd, err := r.svc.ApplySuggestions(r.owner, r.name, "feature", []SuggestionEdit{{Path: "f.txt", Line: 1, Replacement: "uno"}}, tipTestAuthor)
		if err != nil {
			t.Fatal(err)
		}
		return r.gitDir, upd.New
	}},
	{"commit file", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		upd, err := r.svc.CommitFile(r.owner, r.name, "main", "web.txt", []byte("web\n"), tipTestAuthor, "Add web.txt")
		if err != nil {
			t.Fatal(err)
		}
		return r.gitDir, upd.New
	}},
	{"edit file", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		upd, err := r.svc.EditFile(r.owner, r.name, "main", "a.txt", "a.txt", blobOf(t, r, "main", "a.txt"), []byte("edited\n"), tipTestAuthor, "Edit a.txt")
		if err != nil {
			t.Fatal(err)
		}
		return r.gitDir, upd.New
	}},
	{"edit file on a new branch", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		upd, err := r.svc.EditFileOnNewBranch(r.owner, r.name, "main", "patch-1", "a.txt", "a.txt", blobOf(t, r, "main", "a.txt"), []byte("edited\n"), tipTestAuthor, "Edit a.txt")
		if err != nil {
			t.Fatal(err)
		}
		return r.gitDir, upd.New
	}},
	{"delete file", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		_, upd, err := r.svc.DeleteFile(r.owner, r.name, "main", "a.txt", blobOf(t, r, "main", "a.txt"), tipTestAuthor, "Delete a.txt")
		if err != nil {
			t.Fatal(err)
		}
		return r.gitDir, upd.New
	}},
	{"wiki save", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		if err := r.svc.WikiPageSave(r.owner, r.name, "Home", "# Home\n", tipTestAuthor, ""); err != nil {
			t.Fatal(err)
		}
		return wikiTip(t, r)
	}},
	{"wiki rename", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		if err := r.svc.WikiPageSave(r.owner, r.name, "Home", "# Home\n", tipTestAuthor, ""); err != nil {
			t.Fatal(err)
		}
		if err := r.svc.WikiPageRename(r.owner, r.name, "Home", "Start", tipTestAuthor, ""); err != nil {
			t.Fatal(err)
		}
		return wikiTip(t, r)
	}},
	{"repository initial commit", func(t *testing.T, r *tipTestRepo) (string, plumbing.Hash) {
		dir := filepath.Join(t.TempDir(), "init.git")
		if _, err := gogit.PlainInit(dir, true); err != nil {
			t.Fatal(err)
		}
		sig := tipTestAuthor.signature(time.Unix(1_700_000_000, 0).UTC())
		if err := seedInitialCommit(dir, "main", sig, r.svc.commits, RepoInitOptions{AddREADME: true}, "alice", "init", ""); err != nil {
			t.Fatal(err)
		}
		repo, err := gogit.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		return dir, branchTip(t, repo, "main")
	}},
}

func wantServerCommitter(t *testing.T, c *object.Commit) {
	t.Helper()
	if c.Author.Name != tipTestAuthor.Name || c.Author.Email != tipTestAuthor.Email {
		t.Errorf("author = %s <%s>, want %s <%s>", c.Author.Name, c.Author.Email, tipTestAuthor.Name, tipTestAuthor.Email)
	}
	if c.Committer.Name != "Cloudzilla" || c.Committer.Email != "noreply@git.example.test" {
		t.Errorf("committer = %s <%s>, want Cloudzilla <noreply@git.example.test>", c.Committer.Name, c.Committer.Email)
	}
	if !c.Committer.When.Equal(c.Author.When) {
		t.Errorf("committer date %v differs from author date %v", c.Committer.When, c.Author.When)
	}
}

func TestServerCommits_SignedByTheInstanceAndVerifiedByNativeGit(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	stack := newInstanceStack(t, testBox(t, testSecretKey))
	ctx := context.Background()

	for _, site := range serverCommitSites {
		t.Run(site.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			r.svc.WithServerCommits(newServerCommitter(stack.signer, serverCommitBaseURL))
			dir, hash := site.make(t, r)

			repo, err := gogit.PlainOpen(dir)
			if err != nil {
				t.Fatal(err)
			}
			commit, err := repo.CommitObject(hash)
			if err != nil {
				t.Fatal(err)
			}
			wantServerCommitter(t, commit)

			key, err := stack.st.InstanceSigningKey.Active(ctx)
			if err != nil || key == nil {
				t.Fatalf("active instance key: %v, %v", key, err)
			}
			signers := filepath.Join(t.TempDir(), "allowed_signers")
			if err := os.WriteFile(signers, []byte("noreply@git.example.test "+key.PublicKey+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(gitBin, "--git-dir", dir, "-c", "gpg.format=ssh", "-c", "gpg.ssh.allowedSignersFile="+signers, "verify-commit", hash.String())
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git verify-commit: %v\n%s", err, out)
			}

			res, err := stack.sigs.Verify(ctx, []*object.Commit{commit})
			if err != nil {
				t.Fatal(err)
			}
			if got := res[hash]; got.Status != SignatureVerified || got.Kind != SignatureKindInstance {
				t.Errorf("verifier = %s/%s (%s), want verified/instance", got.Status, got.Kind, got.Reason)
			}
		})
	}
}

func TestServerCommits_WithoutASecretKeyAreUnsignedButStillInstanceCommitted(t *testing.T) {
	for _, site := range serverCommitSites {
		t.Run(site.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			r.svc.WithServerCommits(newServerCommitter(NewInstanceSigner(nil, nil), serverCommitBaseURL))
			dir, hash := site.make(t, r)

			repo, err := gogit.PlainOpen(dir)
			if err != nil {
				t.Fatal(err)
			}
			commit, err := repo.CommitObject(hash)
			if err != nil {
				t.Fatal(err)
			}
			wantServerCommitter(t, commit)
			if commit.PGPSignature != "" {
				t.Errorf("commit is signed without a secret key: %q", commit.PGPSignature)
			}
		})
	}
}

type countingHandler struct{ warns *int }

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		*h.warns++
	}
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

func TestWarnUndecryptableKeyOncePerOutage(t *testing.T) {
	warns := 0
	prev := slog.Default()
	slog.SetDefault(slog.New(countingHandler{&warns}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	undecryptableWarned.Store(false)

	for range 3 {
		noteInstanceKeyOutcome(secretbox.ErrUndecryptable)
	}
	if warns != 1 {
		t.Fatalf("warns = %d, want 1 for a persistent outage", warns)
	}
	noteInstanceKeyOutcome(nil)
	noteInstanceKeyOutcome(secretbox.ErrUndecryptable)
	if warns != 2 {
		t.Errorf("warns = %d, want 2 after the key became readable and failed again", warns)
	}
}
