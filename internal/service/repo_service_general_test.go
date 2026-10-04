package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// newGeneralSettingsRepo creates an initialized repo whose main tip also
// carries a dev branch and a v1 tag.
func newGeneralSettingsRepo(t *testing.T) (*service.RepoService, *model.Repository, int64, *gogit.Repository) {
	t.Helper()
	svc, ownerID, owner, root := newRepoSvc(t)
	repo, err := svc.Create(context.Background(), ownerID, owner, "settingsrepo", "before", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	bare, err := gogit.PlainOpen(filepath.Join(root, owner, repo.Name+".git"))
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	main, err := bare.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	for _, name := range []plumbing.ReferenceName{plumbing.NewBranchReferenceName("dev"), plumbing.NewTagReferenceName("v1")} {
		if err := bare.Storer.SetReference(plumbing.NewHashReference(name, main.Hash())); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	return svc, repo, ownerID, bare
}

func newEmptySettingsRepo(t *testing.T) (*service.RepoService, *model.Repository, int64, *gogit.Repository) {
	t.Helper()
	svc, ownerID, owner, root := newRepoSvc(t)
	repo, err := svc.Create(context.Background(), ownerID, owner, "emptysettings", "before", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	bare, err := gogit.PlainOpen(filepath.Join(root, owner, repo.Name+".git"))
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	return svc, repo, ownerID, bare
}

func bareHeadTarget(t *testing.T, bare *gogit.Repository) plumbing.ReferenceName {
	t.Helper()
	head, err := bare.Storer.Reference(plumbing.HEAD)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	if head.Type() != plumbing.SymbolicReference {
		t.Fatalf("HEAD should be symbolic, got %s", head)
	}
	return head.Target()
}

func assertGeneralSettings(t *testing.T, svc *service.RepoService, repoID int64, wantDescription, wantBranch string) {
	t.Helper()
	got, err := svc.GetByID(context.Background(), repoID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Description != wantDescription {
		t.Errorf("description: want %q, got %q", wantDescription, got.Description)
	}
	if got.DefaultBranch != wantBranch {
		t.Errorf("default_branch column: want %q, got %q", wantBranch, got.DefaultBranch)
	}
}

func assertBareHead(t *testing.T, bare *gogit.Repository, wantBranch string) {
	t.Helper()
	if head, want := bareHeadTarget(t, bare), plumbing.NewBranchReferenceName(wantBranch); head != want {
		t.Errorf("bare HEAD: want %s, got %s", want, head)
	}
}

func TestRepoService_UpdateGeneral_ExistingBranchMovesColumnAndHead(t *testing.T) {
	svc, repo, ownerID, bare := newGeneralSettingsRepo(t)

	if err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "after", "", " dev "); err != nil {
		t.Fatalf("UpdateGeneral: %v", err)
	}

	assertGeneralSettings(t, svc, repo.ID, "after", "dev")
	assertBareHead(t, bare, "dev")
}

func TestRepoService_UpdateGeneral_RefusesMissingBranch(t *testing.T) {
	svc, repo, ownerID, bare := newGeneralSettingsRepo(t)

	err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "after", "", "nope")

	if !errors.Is(err, service.ErrInvalidDefaultBranch) {
		t.Fatalf("want ErrInvalidDefaultBranch, got %v", err)
	}
	assertGeneralSettings(t, svc, repo.ID, "before", "main")
	assertBareHead(t, bare, "main")
}

func TestRepoService_UpdateGeneral_RefusesTagName(t *testing.T) {
	svc, repo, ownerID, bare := newGeneralSettingsRepo(t)

	err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "after", "", "v1")

	if !errors.Is(err, service.ErrInvalidDefaultBranch) {
		t.Fatalf("want ErrInvalidDefaultBranch, got %v", err)
	}
	assertGeneralSettings(t, svc, repo.ID, "before", "main")
	assertBareHead(t, bare, "main")
}

func TestRepoService_UpdateGeneral_RefusesUnchangedMissingBranch(t *testing.T) {
	svc, repo, ownerID, bare := newGeneralSettingsRepo(t)
	if err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "before", "", "dev"); err != nil {
		t.Fatalf("UpdateGeneral to dev: %v", err)
	}
	if err := bare.Storer.RemoveReference(plumbing.NewBranchReferenceName("dev")); err != nil {
		t.Fatalf("delete dev: %v", err)
	}

	err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "after", "", "")

	if !errors.Is(err, service.ErrInvalidDefaultBranch) {
		t.Fatalf("want ErrInvalidDefaultBranch, got %v", err)
	}
	assertGeneralSettings(t, svc, repo.ID, "before", "dev")
}

func TestRepoService_UpdateGeneral_EmptyRepoAcceptsNewBranchName(t *testing.T) {
	svc, repo, ownerID, bare := newEmptySettingsRepo(t)

	if err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "after", "", "trunk"); err != nil {
		t.Fatalf("UpdateGeneral: %v", err)
	}

	assertGeneralSettings(t, svc, repo.ID, "after", "trunk")
	assertBareHead(t, bare, "trunk")
}

func TestRepoService_UpdateGeneral_EmptyRepoRefusesMalformedName(t *testing.T) {
	svc, repo, ownerID, bare := newEmptySettingsRepo(t)
	headBefore := bareHeadTarget(t, bare)

	err := svc.UpdateGeneral(context.Background(), repo.ID, ownerID, "after", "", "bad name")

	if !errors.Is(err, service.ErrInvalidDefaultBranch) {
		t.Fatalf("want ErrInvalidDefaultBranch, got %v", err)
	}
	assertGeneralSettings(t, svc, repo.ID, "before", "main")
	if head := bareHeadTarget(t, bare); head != headBefore {
		t.Errorf("bare HEAD moved from %s to %s", headBefore, head)
	}
}
