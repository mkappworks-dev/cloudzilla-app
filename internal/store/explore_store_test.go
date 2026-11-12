package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestExploreStore_ListPublicRepos_PagesAndFilters(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ctx := context.Background()
	explore := store.NewExploreStore(db)
	owner := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	for _, r := range []struct{ name, desc, lang string }{
		{"alpha", "100% pure", "Go"},
		{"beta", "plain", "Go"},
		{"gamma", "plain", "Rust"},
	} {
		testutil.Exec(t, db, `INSERT INTO repositories (owner_id, owner_name, name, description, private, primary_language)
			VALUES ($1, 'o', $2, $3, false, $4)`, owner, r.name, r.desc, r.lang)
	}
	testutil.Exec(t, db, `INSERT INTO repositories (owner_id, owner_name, name, description, private, primary_language)
		VALUES ($1, 'o', 'hidden', '', true, 'Go')`, owner)

	page := func(order store.ExploreOrder, f store.ExploreFilter) ([]string, int) {
		t.Helper()
		repos, total, err := explore.ListPublicRepos(ctx, order, time.Now().Add(-time.Hour), f)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, r := range repos {
			names = append(names, r.Name)
		}
		return names, total
	}

	first, total := page(store.ExploreByNewest, store.ExploreFilter{Limit: 2})
	second, _ := page(store.ExploreByNewest, store.ExploreFilter{Limit: 2, Offset: 2})
	if total != 3 || len(first) != 2 || len(second) != 1 {
		t.Fatalf("pages = %v / %v of %d, want 2 + 1 of 3 (private repo excluded)", first, second, total)
	}
	if first[0] == second[0] || first[1] == second[0] {
		t.Errorf("pages overlap: %v %v", first, second)
	}

	if names, total := page(store.ExploreByTrending, store.ExploreFilter{Language: "Go", Limit: 10}); total != 2 || len(names) != 2 {
		t.Errorf("language Go: %v of %d, want 2", names, total)
	}
	if names, _ := page(store.ExploreByForks, store.ExploreFilter{Text: "100%", Limit: 10}); len(names) != 1 || names[0] != "alpha" {
		t.Errorf("a typed %% must match literally, got %v", names)
	}
	if names, _ := page(store.ExploreByNewest, store.ExploreFilter{Text: "PLAIN", Language: "Rust", Limit: 10}); len(names) != 1 || names[0] != "gamma" {
		t.Errorf("text + language: %v, want [gamma]", names)
	}

	langs, err := explore.PublicLanguages(ctx)
	if err != nil || len(langs) != 2 || langs[0] != "Go" {
		t.Errorf("languages = %v, %v; want [Go Rust]", langs, err)
	}
}
