package store_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A suggestion is a name the viewer can click, so it must follow the same
// visibility rules as the page it links to.
func TestSuggestRepos_HidesWhatTheViewerCannotRead(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	prefix := "sgst" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "sgstowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	readerID := testutil.SeedUser(t, db, "sgstreader_"+suffix)
	strangerID := testutil.SeedUser(t, db, "sgststranger_"+suffix)
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID, readerID, strangerID) })

	mkRepo := func(name string, private, deleted bool) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name, private, deleted_at, deleted_by)
			 VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN NOW() END, CASE WHEN $5 THEN $1::bigint END)
			 RETURNING id`,
			ownerID, ownerName, prefix+name, private, deleted,
		).Scan(&id); err != nil {
			t.Fatalf("insert repo %s: %v", name, err)
		}
		return id
	}
	mkRepo("pub", false, false)
	private := mkRepo("priv", true, false)
	mkRepo("binned", false, true)
	testutil.Exec(t, db,
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'reader')`,
		readerID, private)

	search := store.NewSearchStore(db)
	suggest := func(t *testing.T, q string, viewer *int64, limit int) []string {
		t.Helper()
		repos, err := search.SuggestRepos(ctx, q, viewer, limit)
		if err != nil {
			t.Fatalf("SuggestRepos(%q): %v", q, err)
		}
		names := make([]string, len(repos))
		for i, r := range repos {
			names[i] = r.Name
		}
		slices.Sort(names)
		return names
	}

	viewers := []struct {
		name string
		id   *int64
		want []string
	}{
		{"anonymous", nil, []string{prefix + "pub"}},
		{"stranger", &strangerID, []string{prefix + "pub"}},
		{"reader", &readerID, []string{prefix + "priv", prefix + "pub"}},
		{"owner", &ownerID, []string{prefix + "priv", prefix + "pub"}},
	}
	for _, v := range viewers {
		t.Run(v.name, func(t *testing.T) {
			if got := suggest(t, prefix, v.id, 10); !slices.Equal(got, v.want) {
				t.Errorf("got %q, want %q", got, v.want)
			}
		})
	}

	t.Run("matches owner/name and ignores case", func(t *testing.T) {
		got := suggest(t, strings.ToUpper(ownerName)+"/"+prefix+"p", &ownerID, 10)
		want := []string{prefix + "priv", prefix + "pub"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("LIKE wildcards in the query match literally", func(t *testing.T) {
		for _, q := range []string{"%", "_", prefix[:3] + "%", prefix[:2] + "_" + prefix[3:]} {
			if got := suggest(t, q, &ownerID, 10); len(got) != 0 {
				t.Errorf("q=%q matched %q", q, got)
			}
		}
	})

	t.Run("respects the limit", func(t *testing.T) {
		if got := suggest(t, prefix, &ownerID, 1); len(got) != 1 {
			t.Errorf("got %d repos, want 1", len(got))
		}
	})
}

func TestSuggestUsers(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	aliceID := testutil.SeedUser(t, db, "sgstalice_"+suffix)
	bobID := testutil.SeedUser(t, db, "sgstbob_"+suffix)
	t.Cleanup(func() { testutil.DeleteUsers(t, db, aliceID, bobID) })

	search := store.NewSearchStore(db)
	usernames := func(t *testing.T, q string, limit int) []string {
		t.Helper()
		users, err := search.SuggestUsers(ctx, q, limit)
		if err != nil {
			t.Fatalf("SuggestUsers(%q): %v", q, err)
		}
		names := make([]string, len(users))
		for i, u := range users {
			names[i] = u.Username
		}
		return names
	}

	// SeedUser names every account testuser_<suffix>, so the `_` after
	// "testuser" is the wildcard-escape check.
	if got, want := usernames(t, "TestUser_sgstalice_"+suffix[:1], 10), []string{"testuser_sgstalice_" + suffix}; !slices.Equal(got, want) {
		t.Errorf("prefix, case-insensitive: got %q, want %q", got, want)
	}
	if got := usernames(t, "testuser%sgstalice", 10); len(got) != 0 {
		t.Errorf("%% matched %q", got)
	}
	if got := usernames(t, "testuserXsgstalice", 10); len(got) != 0 {
		t.Errorf("a literal X matched %q", got)
	}
	if got := usernames(t, "testuser_sgst", 1); len(got) != 1 {
		t.Errorf("limit: got %d users, want 1", len(got))
	}

	var ghost string
	if err := db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ghost_user_id()`).Scan(&ghost); err != nil {
		t.Fatalf("ghost username: %v", err)
	}
	if got := usernames(t, ghost, 10); len(got) != 0 {
		t.Errorf("ghost suggested: %q", got)
	}
}

func TestSuggestOrgs(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	prefix := "sgst" + strings.ReplaceAll(suffix, "_", "x")

	var orgID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO organizations (name, display_name) VALUES ($1, $2) RETURNING id`,
		prefix+"org", "Zebra "+prefix,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, orgID) })

	search := store.NewSearchStore(db)
	for name, q := range map[string]string{
		"by name":                   prefix,
		"by name, upper case":       strings.ToUpper(prefix),
		"by display name":           "zebra " + prefix,
		"by display name, mid-case": "ZEBRA " + prefix[:6],
	} {
		t.Run(name, func(t *testing.T) {
			orgs, err := search.SuggestOrgs(ctx, q, 10)
			if err != nil {
				t.Fatalf("SuggestOrgs(%q): %v", q, err)
			}
			if len(orgs) != 1 || orgs[0].ID != orgID {
				t.Errorf("got %+v, want org %d", orgs, orgID)
			}
		})
	}

	for _, q := range []string{"%", "_", prefix[:3] + "%"} {
		if orgs, err := search.SuggestOrgs(ctx, q, 10); err != nil || len(orgs) != 0 {
			t.Errorf("q=%q: got %d orgs, err %v; want none", q, len(orgs), err)
		}
	}
}
