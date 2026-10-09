package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSearch_TypeSelectsEntityKinds(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "srchsvc" + strings.ReplaceAll(suffix, "_", "x")

	ownerID := testutil.SeedUser(t, db, word)
	ownerName := "testuser_" + word
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, $2, $3, false) RETURNING id`,
		ownerID, ownerName, word+"repo").Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repoID) })
	testutil.Exec(t, db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 1, $2, $3, '', 'open', 'public')`,
		repoID, ownerID, word+" issue")
	testutil.Exec(t, db,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, head_branch, base_branch) VALUES ($1, 2, $2, $3, '', 'f', 'main')`,
		repoID, ownerID, word+" pull")

	svc := service.NewSearchService(store.NewSearchStore(db))

	// SearchUsers matches username prefixes, so the user query is the seeded username.
	cases := []struct {
		typ                           string
		repos, issues, pulls, userHit bool
	}{
		{"all", true, true, true, false},
		{"", true, true, true, false},
		{"repos", true, false, false, false},
		{"issues", false, true, false, false},
		{"pulls", false, false, true, false},
		{"users", false, false, false, false},
	}
	for _, c := range cases {
		t.Run("type="+c.typ, func(t *testing.T) {
			got, err := svc.Search(ctx, word, c.typ, nil)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if got.Query != word || got.Type != c.typ {
				t.Errorf("echoed query/type = %q/%q", got.Query, got.Type)
			}
			if (len(got.Repos) > 0) != c.repos || (len(got.Issues) > 0) != c.issues ||
				(len(got.Pulls) > 0) != c.pulls || (len(got.Users) > 0) != c.userHit {
				t.Errorf("repos=%d issues=%d pulls=%d users=%d, want kinds %+v",
					len(got.Repos), len(got.Issues), len(got.Pulls), len(got.Users), c)
			}
		})
	}

	t.Run("users type matches username prefix", func(t *testing.T) {
		got, err := svc.Search(ctx, "TestUser_"+word, "users", nil)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got.Users) != 1 || got.Users[0].ID != ownerID {
			t.Errorf("users = %+v, want just the seeded owner", got.Users)
		}
		if len(got.Repos)+len(got.Issues)+len(got.Pulls) != 0 {
			t.Error("users search must not return other kinds")
		}
	})

	t.Run("unknown type searches nothing", func(t *testing.T) {
		got, err := svc.Search(ctx, word, "bogus", nil)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got.Repos)+len(got.Issues)+len(got.Pulls)+len(got.Users) != 0 {
			t.Errorf("unexpected results: %+v", got)
		}
	})
}

func TestSearch_PrivateRepoHiddenFromAnonymous(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "srchpriv" + strings.ReplaceAll(suffix, "_", "x")
	ownerID := testutil.SeedUser(t, db, word)
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, $2, $3, true) RETURNING id`,
		ownerID, "testuser_"+word, word+"secret").Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repoID) })

	svc := service.NewSearchService(store.NewSearchStore(db))
	anon, err := svc.Search(ctx, word, "repos", nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(anon.Repos) != 0 {
		t.Errorf("anonymous viewer saw %d private repos", len(anon.Repos))
	}
	owner, err := svc.Search(ctx, word, "repos", &ownerID)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(owner.Repos) != 1 {
		t.Errorf("owner saw %d repos, want 1", len(owner.Repos))
	}
}

func TestSearch_StoreFailureOmitsResultsWithoutError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewSearchService(store.NewSearchStore(db))
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := svc.Search(context.Background(), "anything", "all", nil)
	if err != nil {
		t.Fatalf("Search must swallow store errors, got %v", err)
	}
	if len(got.Repos)+len(got.Issues)+len(got.Pulls)+len(got.Users) != 0 {
		t.Errorf("results = %+v, want empty", got)
	}
}
