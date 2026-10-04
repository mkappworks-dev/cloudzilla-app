package service_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func TestRepoService_Fork_IntoAnOwnedOrgWithNameAndDescription(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	orig, err := env.repos.Create(ctx, ownerID, owner, "upstream", "the original", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := env.repos.AddCollaborator(ctx, orig, ownerID, user, string(model.RoleReader)); err != nil {
		t.Fatalf("grant read: %v", err)
	}
	org := env.createOrg(t, userID)
	desc := "my copy"

	forked, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Owner: org.Name, Name: "downstream", Description: &desc})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	got, err := env.repos.Get(ctx, org.Name, "downstream")
	if err != nil {
		t.Fatalf("fork not found under the org: %v", err)
	}
	if got.ID != forked.ID || got.OrgID != org.ID || got.CreatedBy != userID {
		t.Errorf("fork row = id %d org %d created_by %d, want id %d org %d created_by %d", got.ID, got.OrgID, got.CreatedBy, forked.ID, org.ID, userID)
	}
	if got.Description != desc || !got.Private || !got.IsFork || got.ForkOfID == nil || *got.ForkOfID != orig.ID {
		t.Errorf("fork row = %+v, want description %q, private, fork of %d", got, desc, orig.ID)
	}
	origDir, _ := env.dirs(owner, "upstream")
	forkDir, _ := env.dirs(org.Name, "downstream")
	if headOf(t, forkDir) != headOf(t, origDir) {
		t.Error("fork does not match the original")
	}
	reread, err := env.repos.Get(ctx, owner, "upstream")
	if err != nil {
		t.Fatal(err)
	}
	if reread.ForkCount != 1 {
		t.Errorf("fork count = %d, want 1", reread.ForkCount)
	}
}

func TestRepoService_Fork_RefusesOrgsTheActorDoesNotOwn(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	if _, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create original: %v", err)
	}
	strangers := env.createOrg(t, ownerID)
	joined := env.createOrg(t, ownerID)
	if err := env.orgs.AddMember(ctx, joined.ID, ownerID, userID, model.OrgRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	for _, org := range []*model.Organization{strangers, joined} {
		_, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Owner: org.Name})
		if !errors.Is(err, service.ErrForbidden) {
			t.Errorf("fork into %s: want ErrForbidden, got %v", org.Name, err)
		}
		if n := env.rowCount(t, org.Name, "upstream"); n != 0 {
			t.Errorf("fork into %s left %d rows", org.Name, n)
		}
	}
}

func TestRepoService_Fork_RefusesTheSourcesOwnNamespace(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	if _, err := env.repos.Create(ctx, ownerID, owner, "mine", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create personal repo: %v", err)
	}
	if _, err := env.orgs.WithRepoService(env.repos).CreateRepo(ctx, org.ID, ownerID, "ours", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create org repo: %v", err)
	}

	cases := []struct{ src, into string }{{owner, ""}, {owner, owner}, {org.Name, org.Name}}
	for _, c := range cases {
		name := map[string]string{owner: "mine", org.Name: "ours"}[c.src]
		_, err := env.repos.Fork(ctx, c.src, name, ownerID, owner, service.ForkOptions{Owner: c.into, Name: "copy"})
		if !errors.Is(err, service.ErrForkIntoSourceOwner) {
			t.Errorf("fork %s/%s into %q: want ErrForkIntoSourceOwner, got %v", c.src, name, c.into, err)
		}
	}
	if n := env.rowCount(t, owner, "copy") + env.rowCount(t, org.Name, "copy"); n != 0 {
		t.Errorf("%d copies were created", n)
	}

	if _, err := env.repos.Fork(ctx, org.Name, "ours", ownerID, owner, service.ForkOptions{}); err != nil {
		t.Errorf("fork an org repo into the owner's account: %v", err)
	}
}

func TestRepoService_Fork_ATakenExplicitNameFails(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	if _, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if _, err := env.repos.Create(ctx, userID, user, "upstream", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create clash: %v", err)
	}

	_, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Name: "upstream"})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if n := env.rowCount(t, user, "upstream-1"); n != 0 {
		t.Error("an explicit name fell back to a suffixed one")
	}

	_, err = env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Name: "../escape"})
	if !errors.Is(err, service.ErrInvalidRepoName) {
		t.Errorf("path as name: want ErrInvalidRepoName, got %v", err)
	}
}

func TestRepoService_Fork_DefaultBranchOnly(t *testing.T) {
	for _, packed := range []bool{false, true} {
		name := "loose refs"
		if packed {
			name = "packed refs"
		}
		t.Run(name, func(t *testing.T) {
			env := newRepoDirsEnv(t)
			ctx := context.Background()
			ownerID, owner := env.seedUser(t)
			userID, user := env.seedUser(t)
			if _, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
				t.Fatalf("create original: %v", err)
			}
			if err := env.code.CreateBranch(owner, "upstream", "feature", "main"); err != nil {
				t.Fatalf("create branch: %v", err)
			}
			if err := env.code.CreateTag(owner, "upstream", "v1", "main"); err != nil {
				t.Fatalf("create tag: %v", err)
			}
			origDir, _ := env.dirs(owner, "upstream")
			if packed {
				packRefs(t, origDir)
			}

			if _, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Name: "all"}); err != nil {
				t.Fatalf("full fork: %v", err)
			}
			if _, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Name: "trunk", DefaultBranchOnly: true}); err != nil {
				t.Fatalf("default-branch fork: %v", err)
			}

			allDir, _ := env.dirs(user, "all")
			trunkDir, _ := env.dirs(user, "trunk")
			want := map[string]map[string]bool{
				allDir:   {"refs/heads/main": true, "refs/heads/feature": true, "refs/tags/v1": true},
				trunkDir: {"refs/heads/main": true, "refs/tags/v1": true},
			}
			for dir, refs := range want {
				got := refNames(t, dir)
				if len(got) != len(refs) {
					t.Errorf("%s refs = %v, want %v", dir, got, refs)
				}
				for _, r := range got {
					if !refs[r] {
						t.Errorf("%s refs = %v, want %v", dir, got, refs)
					}
				}
				if headOf(t, dir) != headOf(t, origDir) {
					t.Errorf("%s HEAD moved off the default branch", dir)
				}
			}
			if refs := refNames(t, origDir); len(refs) != 3 {
				t.Errorf("the original lost refs: %v", refs)
			}
		})
	}
}

// A source's settings can name any default branch, and HEAD doesn't follow.
func TestRepoService_Fork_DefaultBranchOnly_FollowsTheSettingsDefault(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	orig, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := env.code.CreateBranch(owner, "upstream", "develop", "main"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := env.code.CommitFile(owner, "upstream", "develop", "NOTES.md", []byte("develop only"), dirsTestAuthor, "work on develop"); err != nil {
		t.Fatalf("commit on develop: %v", err)
	}
	if err := env.code.CreateTag(owner, "upstream", "v1", "main"); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if err := env.repos.UpdateGeneral(ctx, orig.ID, ownerID, "", "", "develop"); err != nil {
		t.Fatalf("set default branch: %v", err)
	}

	if _, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{DefaultBranchOnly: true}); err != nil {
		t.Fatalf("Fork: %v", err)
	}

	forkDir, _ := env.dirs(user, "upstream")
	got := refNames(t, forkDir)
	if len(got) != 2 || !slices.Contains(got, "refs/heads/develop") || !slices.Contains(got, "refs/tags/v1") {
		t.Errorf("fork refs = %v, want refs/heads/develop and refs/tags/v1", got)
	}
	repo, err := gogit.PlainOpen(forkDir)
	if err != nil {
		t.Fatalf("open fork: %v", err)
	}
	head, err := repo.Storer.Reference(plumbing.HEAD)
	if err != nil || head.Target() != plumbing.NewBranchReferenceName("develop") {
		t.Errorf("fork HEAD = %v (%v), want a symbolic ref to refs/heads/develop", head, err)
	}
	develop, err := repo.Reference(plumbing.NewBranchReferenceName("develop"), true)
	if err != nil {
		t.Fatalf("develop in fork: %v", err)
	}
	if got := headOf(t, forkDir); got != develop.Hash().String() {
		t.Errorf("fork HEAD resolves to %s, want develop at %s", got, develop.Hash())
	}
}

func TestRepoService_Fork_DefaultBranchOnly_RefusesAMissingDefaultBranch(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	orig, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	// Settings refuse this now; older rows and a push that deletes the branch still leave it.
	if _, err := env.db.ExecContext(ctx, `UPDATE repositories SET default_branch = 'gone' WHERE id = $1`, orig.ID); err != nil {
		t.Fatalf("set default branch: %v", err)
	}

	_, err = env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{DefaultBranchOnly: true})

	if !errors.Is(err, service.ErrForkDefaultBranchMissing) {
		t.Fatalf("want ErrForkDefaultBranchMissing, got %v", err)
	}
	if err != service.ErrForkDefaultBranchMissing {
		t.Errorf("error = %q, want the sentinel's own sentence", err)
	}
	forkDir, _ := env.dirs(user, "upstream")
	if n := env.rowCount(t, user, "upstream"); n != 0 || pathExists(forkDir) {
		t.Errorf("refused fork left %d rows, dir exists %v", n, pathExists(forkDir))
	}
	reread, err := env.repos.Get(ctx, owner, "upstream")
	if err != nil {
		t.Fatal(err)
	}
	if reread.ForkCount != 0 {
		t.Errorf("fork count = %d, want 0", reread.ForkCount)
	}
	if _, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{}); err != nil {
		t.Errorf("a full fork of the same source after the refusal: %v", err)
	}
}

func TestRepoService_Fork_DefaultBranchOnly_OfAnEmptySource(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	if _, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create original: %v", err)
	}

	if _, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{DefaultBranchOnly: true}); err != nil {
		t.Fatalf("Fork: %v", err)
	}

	forkDir, _ := env.dirs(user, "upstream")
	if got := refNames(t, forkDir); len(got) != 0 {
		t.Errorf("fork of an empty source has refs %v", got)
	}
}

func TestCodeService_HasBranch(t *testing.T) {
	env := newRepoDirsEnv(t)
	ownerID, owner := env.seedUser(t)
	if _, err := env.repos.Create(context.Background(), ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := env.code.CreateTag(owner, "upstream", "v1", "main"); err != nil {
		t.Fatalf("create tag: %v", err)
	}

	for name, want := range map[string]bool{"main": true, "v1": false, "gone": false, "": false} {
		if got := env.code.HasBranch(owner, "upstream", name); got != want {
			t.Errorf("HasBranch(%q) = %v, want %v", name, got, want)
		}
	}
	if env.code.HasBranch(owner, "missing", "main") {
		t.Error("HasBranch of a missing repo = true")
	}
}

// packRefs leaves the branches only in packed-refs, so the fork prunes a
// source with no loose ref files.
func packRefs(t *testing.T, gitDir string) {
	t.Helper()
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		t.Fatalf("open %s: %v", gitDir, err)
	}
	if err := repo.Storer.(*filesystem.Storage).PackRefs(); err != nil {
		t.Fatalf("pack refs: %v", err)
	}
	for _, sub := range []string{"heads", "tags"} {
		_ = filepath.WalkDir(filepath.Join(gitDir, "refs", sub), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				t.Fatalf("loose ref %s survived packing", path)
			}
			return nil
		})
	}
}

func refNames(t *testing.T, gitDir string) []string {
	t.Helper()
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		t.Fatalf("open %s: %v", gitDir, err)
	}
	iter, err := repo.References()
	if err != nil {
		t.Fatalf("list refs: %v", err)
	}
	var names []string
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		if r.Name() != plumbing.HEAD {
			names = append(names, r.Name().String())
		}
		return nil
	})
	return names
}

func TestRepoService_ForksOwnedBy(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	otherID, other := env.seedUser(t)
	orig, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	owned := env.createOrg(t, userID)
	joined := env.createOrg(t, otherID)
	if err := env.orgs.AddMember(ctx, joined.ID, otherID, userID, model.OrgRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	fork := func(actorID int64, actor, into, name string) *model.Repository {
		t.Helper()
		r, err := env.repos.Fork(ctx, owner, "upstream", actorID, actor, service.ForkOptions{Owner: into, Name: name})
		if err != nil {
			t.Fatalf("fork %s/%s: %v", into, name, err)
		}
		return r
	}
	mine := fork(userID, user, "", "mine")
	ours := fork(userID, user, owned.Name, "ours")
	fork(otherID, other, joined.Name, "theirs")
	fork(otherID, other, "", "elsewhere")
	gone := fork(userID, user, "", "gone")
	if err := env.repos.Delete(ctx, gone.ID, userID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := env.repos.Create(ctx, userID, user, "plain", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create plain repo: %v", err)
	}
	if _, err := env.repos.Create(ctx, otherID, other, "elsewhere-upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create other upstream: %v", err)
	}
	if _, err := env.repos.Fork(ctx, other, "elsewhere-upstream", userID, user, service.ForkOptions{Name: "of-another"}); err != nil {
		t.Fatalf("fork another upstream: %v", err)
	}

	got, err := env.repos.ForksOwnedBy(ctx, orig.ID, userID)
	if err != nil {
		t.Fatalf("ForksOwnedBy: %v", err)
	}
	ids := map[int64]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if len(got) != 2 || !ids[mine.ID] || !ids[ours.ID] {
		t.Errorf("ForksOwnedBy = %v, want %s/mine and %s/ours", got, user, owned.Name)
	}
}
