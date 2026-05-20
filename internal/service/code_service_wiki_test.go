package service

import (
	"reflect"
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

	pages, err := svc.WikiPageListMeta("nobody", "norepo")
	if err != nil {
		t.Fatalf("expected nil error for missing wiki, got: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected empty slice, got %d pages", len(pages))
	}
}

func TestWikiPageRename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})

	owner := "bob"
	repo := "wikirepo"
	content := "# My Page\n\nContent here.\n"

	if err := svc.WikiPageSave(owner, repo, "OldName", content, "Tester", "tester@example.com", "add OldName"); err != nil {
		t.Fatalf("WikiPageSave: %v", err)
	}

	if err := svc.WikiPageRename(owner, repo, "OldName", "NewName", "Tester", "tester@example.com", "Rename OldName to NewName"); err != nil {
		t.Fatalf("WikiPageRename: %v", err)
	}

	got, found, err := svc.WikiPageGet(owner, repo, "NewName")
	if err != nil {
		t.Fatalf("WikiPageGet NewName: %v", err)
	}
	if !found {
		t.Fatal("NewName not found after rename")
	}
	if got != content {
		t.Errorf("NewName content = %q, want %q", got, content)
	}

	_, found, err = svc.WikiPageGet(owner, repo, "OldName")
	if err != nil {
		t.Fatalf("WikiPageGet OldName: %v", err)
	}
	if found {
		t.Error("OldName still present after rename")
	}

	if err := svc.WikiPageSave(owner, repo, "Existing", "# Existing\n", "Tester", "tester@example.com", "add Existing"); err != nil {
		t.Fatalf("WikiPageSave Existing: %v", err)
	}
	err = svc.WikiPageRename(owner, repo, "NewName", "Existing", "Tester", "tester@example.com", "")
	if err == nil {
		t.Error("expected error on collision, got nil")
	}

	err = svc.WikiPageRename(owner, repo, "DoesNotExist", "Whatever", "Tester", "tester@example.com", "")
	if err == nil {
		t.Error("expected error for missing source page, got nil")
	}
}

func TestOrderWikiSlugs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		all     []string
		order   string
		want    []string
	}{
		{
			name:  "no order file — alphabetical",
			all:   []string{"Home", "Architecture", "Contributing"},
			order: "",
			want:  []string{"Architecture", "Contributing", "Home"},
		},
		{
			name:  "full explicit order",
			all:   []string{"Home", "Architecture", "Contributing"},
			order: "Home\nArchitecture\nContributing",
			want:  []string{"Home", "Architecture", "Contributing"},
		},
		{
			name:  "partial order — unlisted pages appended alphabetically",
			all:   []string{"Home", "Architecture", "Contributing", "Zzzz"},
			order: "Zzzz\nHome",
			want:  []string{"Zzzz", "Home", "Architecture", "Contributing"},
		},
		{
			name:  "stale entry in order file is skipped",
			all:   []string{"Home", "Architecture"},
			order: "Deleted\nHome\nArchitecture",
			want:  []string{"Home", "Architecture"},
		},
		{
			name:  "duplicate in order file — only first occurrence kept",
			all:   []string{"Home", "Architecture"},
			order: "Home\nHome\nArchitecture",
			want:  []string{"Home", "Architecture"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := orderWikiSlugs(tc.all, tc.order)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("orderWikiSlugs(%v, %q) = %v, want %v", tc.all, tc.order, got, tc.want)
			}
		})
	}
}

func TestWikiPageRename_RewritesOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})

	owner := "dave"
	repo := "renameorder"
	for _, slug := range []string{"Alpha", "Beta", "Gamma"} {
		if err := svc.WikiPageSave(owner, repo, slug, "# "+slug+"\n", "Tester", "tester@example.com", "add "+slug); err != nil {
			t.Fatalf("WikiPageSave %s: %v", slug, err)
		}
	}
	if err := svc.WikiPageSetOrder(owner, repo, []string{"Gamma", "Alpha", "Beta"}, "Tester", "tester@example.com"); err != nil {
		t.Fatalf("WikiPageSetOrder: %v", err)
	}

	if err := svc.WikiPageRename(owner, repo, "Alpha", "Aardvark", "Tester", "tester@example.com", ""); err != nil {
		t.Fatalf("WikiPageRename: %v", err)
	}

	slugs, err := svc.WikiPageList(owner, repo)
	if err != nil {
		t.Fatalf("WikiPageList: %v", err)
	}
	if !reflect.DeepEqual(slugs, []string{"Gamma", "Aardvark", "Beta"}) {
		t.Errorf("post-rename order = %v, want [Gamma Aardvark Beta]", slugs)
	}
}

func TestWikiPageDelete_StripsOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})

	owner := "eve"
	repo := "deleteorder"
	for _, slug := range []string{"Alpha", "Beta", "Gamma"} {
		if err := svc.WikiPageSave(owner, repo, slug, "# "+slug+"\n", "Tester", "tester@example.com", "add "+slug); err != nil {
			t.Fatalf("WikiPageSave %s: %v", slug, err)
		}
	}
	if err := svc.WikiPageSetOrder(owner, repo, []string{"Gamma", "Alpha", "Beta"}, "Tester", "tester@example.com"); err != nil {
		t.Fatalf("WikiPageSetOrder: %v", err)
	}

	if err := svc.WikiPageDelete(owner, repo, "Alpha", "Tester", "tester@example.com"); err != nil {
		t.Fatalf("WikiPageDelete: %v", err)
	}

	slugs, err := svc.WikiPageList(owner, repo)
	if err != nil {
		t.Fatalf("WikiPageList: %v", err)
	}
	// Alpha is gone; Gamma stays first per .order; Beta picks up after it.
	if !reflect.DeepEqual(slugs, []string{"Gamma", "Beta"}) {
		t.Errorf("post-delete order = %v, want [Gamma Beta]", slugs)
	}
}

func TestWikiPageSetOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})

	owner := "carol"
	repo := "wikisetorder"

	for _, slug := range []string{"Alpha", "Beta", "Gamma"} {
		if err := svc.WikiPageSave(owner, repo, slug, "# "+slug+"\n", "Tester", "tester@example.com", "add "+slug); err != nil {
			t.Fatalf("WikiPageSave %s: %v", slug, err)
		}
	}

	slugs, err := svc.WikiPageList(owner, repo)
	if err != nil {
		t.Fatalf("WikiPageList: %v", err)
	}
	if !reflect.DeepEqual(slugs, []string{"Alpha", "Beta", "Gamma"}) {
		t.Fatalf("initial order = %v, want [Alpha Beta Gamma]", slugs)
	}

	if err := svc.WikiPageSetOrder(owner, repo, []string{"Gamma", "Alpha", "Beta"}, "Tester", "tester@example.com"); err != nil {
		t.Fatalf("WikiPageSetOrder: %v", err)
	}
	slugs, err = svc.WikiPageList(owner, repo)
	if err != nil {
		t.Fatalf("WikiPageList after SetOrder: %v", err)
	}
	if !reflect.DeepEqual(slugs, []string{"Gamma", "Alpha", "Beta"}) {
		t.Errorf("after SetOrder = %v, want [Gamma Alpha Beta]", slugs)
	}

	if err := svc.WikiPageSetOrder(owner, repo, []string{"Beta", "Gamma", "Alpha"}, "Tester", "tester@example.com"); err != nil {
		t.Fatalf("WikiPageSetOrder second call: %v", err)
	}
	slugs, _ = svc.WikiPageList(owner, repo)
	if !reflect.DeepEqual(slugs, []string{"Beta", "Gamma", "Alpha"}) {
		t.Errorf("after second SetOrder = %v, want [Beta Gamma Alpha]", slugs)
	}

	if err := svc.WikiPageSetOrder("nobody", "norepo", []string{"X"}, "Tester", "tester@example.com"); err == nil {
		t.Error("expected error for missing wiki repo, got nil")
	}
}
