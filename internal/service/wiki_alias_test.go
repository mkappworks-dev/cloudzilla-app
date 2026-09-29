package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedWiki gives owner/repo a wiki with one page, on disk at repo.wiki.git.
func seedWiki(t *testing.T, root, owner, repo string) {
	t.Helper()
	code := NewCodeService(config.GitConfig{ReposRoot: root})
	if err := code.WikiPageSave(owner, repo, "Home", "# Home\n", wikiTestAuthor, "add Home"); err != nil {
		t.Fatalf("seed wiki %s/%s: %v", owner, repo, err)
	}
}

func TestGet_RepoNamedLikeAWikiIsNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t)
	owner := "wikialias_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	seedRepoRow(t, db, ownerID, owner, "x", "NULL")
	svc := newDiskRepoService(db, t.TempDir())

	for _, name := range []string{"x.wiki", "x.WIKI"} {
		seedRepoRow(t, db, ownerID, owner, name, "NULL")
		if _, err := svc.Get(context.Background(), owner, name); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("Get(%q): want sql.ErrNoRows, got %v", name, err)
		}
	}
	if _, err := svc.Get(context.Background(), owner, "x"); err != nil {
		t.Errorf("Get(x): %v", err)
	}
}

func TestCodeService_RepoNamedLikeAWikiIsNotOpened(t *testing.T) {
	root := t.TempDir()
	seedWiki(t, root, "alice", "x")
	code := NewCodeService(config.GitConfig{ReposRoot: root})

	for _, name := range []string{"x.wiki", "x.WIKI"} {
		if _, _, err := code.ResolveRef("alice", name, ""); !errors.Is(err, gogit.ErrRepositoryNotExists) {
			t.Errorf("ResolveRef(%q): want ErrRepositoryNotExists, got %v", name, err)
		}
	}
	pages, err := code.WikiPageList("alice", "x")
	if err != nil || len(pages) != 1 || pages[0] != "Home" {
		t.Errorf("x's wiki must still open: pages %v, err %v", pages, err)
	}
}

func TestFork_RepoNamedLikeAWikiIsNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	suffix := testutil.UniqueSuffix(t)
	owner, forker := "wikialias_"+suffix, "forker_"+suffix
	ownerID := seedOwner(t, db, owner)
	forkerID := seedOwner(t, db, forker)
	seedRepoRow(t, db, ownerID, owner, "x.wiki", "NULL")
	seedWiki(t, root, owner, "x")

	_, err := newDiskRepoService(db, root).Fork(context.Background(), owner, "x.wiki", forkerID, forker)

	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want sql.ErrNoRows, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, forker, "x.wiki.git")); !os.IsNotExist(err) {
		t.Errorf("x's wiki must not be copied into the fork; stat: %v", err)
	}
}

func TestCreateFromTemplate_TemplateNamedLikeAWikiIsNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner := "wikialias_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	tmplID := seedRepoRow(t, db, ownerID, owner, "x.wiki", "NULL")
	testutil.Exec(t, db, `UPDATE repositories SET is_template = true WHERE id = $1`, tmplID)
	seedWiki(t, root, owner, "x")

	_, err := newDiskRepoService(db, root).CreateFromTemplate(context.Background(), tmplID, ownerID, owner, "copy", "")

	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("want ErrTemplateNotFound, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, owner, "copy.git")); !os.IsNotExist(err) {
		t.Errorf("x's wiki must not be copied into a new repo; stat: %v", err)
	}
}
