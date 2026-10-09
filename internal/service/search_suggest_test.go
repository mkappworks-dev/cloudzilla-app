package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSuggest_ShortQueryNeverReachesTheStore(t *testing.T) {
	// A nil store panics on any query, so passing proves none was made.
	svc := service.NewSearchService(nil)
	for _, q := range []string{"", "a", "  a  ", "\t\n"} {
		got, err := svc.Suggest(context.Background(), q, nil)
		if err != nil {
			t.Fatalf("Suggest(%q): %v", q, err)
		}
		if len(got.Repos)+len(got.Users)+len(got.Orgs) != 0 {
			t.Errorf("Suggest(%q) = %+v, want empty", q, got)
		}
	}
}

func TestSuggest_GroupsRepoUserAndOrg(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "sgsvc" + strings.ReplaceAll(suffix, "_", "x")

	// Not SeedUser: it prefixes every username with "testuser_".
	ownerName := word + "user"
	var ownerID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false) RETURNING id`,
		ownerName, ownerName+"@test.invalid",
	).Scan(&ownerID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID) })
	testutil.Exec(t, db,
		`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, $2, $3, true)`,
		ownerID, ownerName, word+"repo")
	testutil.Exec(t, db, `INSERT INTO organizations (name) VALUES ($1)`, word+"org")
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, word+"org") })

	svc := service.NewSearchService(store.NewSearchStore(db))

	got, err := svc.Suggest(ctx, "  "+strings.ToUpper(word)+"  ", &ownerID)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if got.Query != strings.ToUpper(word) {
		t.Errorf("Query = %q, want the trimmed input", got.Query)
	}
	if len(got.Repos) != 1 || len(got.Orgs) != 1 || len(got.Users) != 1 {
		t.Fatalf("got %d repos, %d orgs, %d users; want 1 of each", len(got.Repos), len(got.Orgs), len(got.Users))
	}

	if hidden, err := svc.Suggest(ctx, word, nil); err != nil || len(hidden.Repos) != 0 {
		t.Errorf("anonymous saw the private repo: %d repos, err %v", len(hidden.Repos), err)
	}

	long, err := svc.Suggest(ctx, strings.Repeat("x", 5000), &ownerID)
	if err != nil {
		t.Fatalf("Suggest(long): %v", err)
	}
	if len(long.Query) > 100 {
		t.Errorf("Query kept %d characters, want at most 100", len(long.Query))
	}
}
