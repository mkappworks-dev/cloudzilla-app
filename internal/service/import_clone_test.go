package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCloneForImport_CopiesBranchesTagsAndHead(t *testing.T) {
	src := testutil.SeedSourceRepo(t)
	url := testutil.ServeGitHTTP(t, src.Dir, "", "")
	dir := filepath.Join(t.TempDir(), "clone")

	branch, err := cloneForImport(context.Background(), dir, url, nil, io.Discard)
	if err != nil {
		t.Fatalf("cloneForImport: %v", err)
	}
	if branch != "develop" {
		t.Errorf("default branch = %q, want develop", branch)
	}

	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open clone: %v", err)
	}
	want := map[plumbing.ReferenceName]plumbing.Hash{
		plumbing.NewBranchReferenceName("main"):    src.First,
		plumbing.NewBranchReferenceName("develop"): src.Second,
		plumbing.NewTagReferenceName("v1"):         src.First,
	}
	for name, hash := range want {
		ref, err := repo.Reference(name, false)
		if err != nil || ref.Hash() != hash {
			t.Errorf("%s = %v, %v; want %s", name, ref, err, hash)
		}
	}
	if _, err := repo.Reference("refs/pull/1/head", false); err == nil {
		t.Error("refs/pull/1/head was imported")
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil || head.Target() != plumbing.NewBranchReferenceName("develop") {
		t.Errorf("HEAD = %v, %v; want -> refs/heads/develop", head, err)
	}
	cfg, err := repo.Config()
	if err != nil || len(cfg.Remotes) != 0 {
		t.Errorf("remotes = %v, %v; want none", cfg.Remotes, err)
	}
	if _, err := repo.CommitObject(src.Second); err != nil {
		t.Errorf("develop's commit missing: %v", err)
	}
}

func TestCloneForImport_PrivateSourceNeedsCredentials(t *testing.T) {
	src := testutil.SeedSourceRepo(t)
	url := testutil.ServeGitHTTP(t, src.Dir, "alice", "s3cret")

	_, err := cloneForImport(context.Background(), filepath.Join(t.TempDir(), "anon"), url, nil, io.Discard)
	if !errors.Is(err, transport.ErrAuthenticationRequired) {
		t.Fatalf("without credentials: err = %v, want ErrAuthenticationRequired", err)
	}
	if _, err := cloneForImport(context.Background(), filepath.Join(t.TempDir(), "authed"), url, importAuth("alice", "s3cret"), io.Discard); err != nil {
		t.Fatalf("with credentials: %v", err)
	}
}

func TestCloneForImport_EmptySource(t *testing.T) {
	dir := t.TempDir()
	if _, err := gogit.PlainInit(dir, true); err != nil {
		t.Fatalf("init: %v", err)
	}
	url := testutil.ServeGitHTTP(t, dir, "", "")
	_, err := cloneForImport(context.Background(), filepath.Join(t.TempDir(), "clone"), url, nil, io.Discard)
	if !errors.Is(err, ErrImportEmptySource) {
		t.Errorf("err = %v, want ErrImportEmptySource", err)
	}
}
