package service

import (
	"testing"
	"time"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestWikiPageListMeta(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})

	owner := "alice"
	repo := "testrepo"

	// Home has a leading H1 heading; Architecture has no heading.
	if err := svc.WikiPageSave(owner, repo, "Home", "# Hello\n\nSome content here.\n", "Tester", "tester@example.com", "add Home"); err != nil {
		t.Fatalf("WikiPageSave Home: %v", err)
	}
	if err := svc.WikiPageSave(owner, repo, "Architecture", "No heading here.\n\nJust plain text.\n", "Tester", "tester@example.com", "add Architecture"); err != nil {
		t.Fatalf("WikiPageSave Architecture: %v", err)
	}

	pages, err := svc.WikiPageListMeta(owner, repo)
	if err != nil {
		t.Fatalf("WikiPageListMeta: %v", err)
	}

	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d: %+v", len(pages), pages)
	}

	if pages[0].Slug != "Architecture" {
		t.Errorf("pages[0].Slug = %q, want %q", pages[0].Slug, "Architecture")
	}
	if pages[1].Slug != "Home" {
		t.Errorf("pages[1].Slug = %q, want %q", pages[1].Slug, "Home")
	}

	// Architecture has no heading — title should fall back to the slug.
	if pages[0].Title != "Architecture" {
		t.Errorf("Architecture Title = %q, want %q", pages[0].Title, "Architecture")
	}

	if pages[1].Title != "Hello" {
		t.Errorf("Home Title = %q, want %q", pages[1].Title, "Hello")
	}

	zero := time.Time{}
	if pages[0].UpdatedAt == zero {
		t.Errorf("Architecture UpdatedAt is zero")
	}
	if pages[1].UpdatedAt == zero {
		t.Errorf("Home UpdatedAt is zero")
	}
}

func TestWikiPageListMeta_Empty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})

	// No wiki repo seeded — should return empty slice, not error.
	pages, err := svc.WikiPageListMeta("nobody", "norepo")
	if err != nil {
		t.Fatalf("expected nil error for missing wiki, got: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected empty slice, got %d pages", len(pages))
	}
}
