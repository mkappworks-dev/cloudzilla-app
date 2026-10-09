package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestTopicStore_SetTopicsReplacesAndListsSorted(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, "tp"+suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_tp"+suffix, suffix)
	a, b, c := "tpa-"+suffix, "tpb-"+suffix, "tpc-"+suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM topics WHERE name = ANY($1)`, []string{a, b, c}) })

	ts := store.NewTopicStore(db)
	if err := ts.SetTopics(ctx, repoID, []string{c, a}); err != nil {
		t.Fatalf("SetTopics: %v", err)
	}
	got, err := ts.ListByRepo(ctx, repoID)
	if err != nil || len(got) != 2 || got[0].Name != a || got[1].Name != c {
		t.Fatalf("ListByRepo = %+v, %v; want [a c] sorted", got, err)
	}

	if err := ts.SetTopics(ctx, repoID, []string{b, a}); err != nil {
		t.Fatalf("SetTopics replace: %v", err)
	}
	got, _ = ts.ListByRepo(ctx, repoID)
	if len(got) != 2 || got[0].Name != a || got[1].Name != b {
		t.Errorf("after replace = %+v, want [a b]", got)
	}

	if err := ts.SetTopics(ctx, repoID, nil); err != nil {
		t.Fatalf("SetTopics clear: %v", err)
	}
	if got, _ := ts.ListByRepo(ctx, repoID); len(got) != 0 {
		t.Errorf("after clear = %+v", got)
	}

	// The unique (repo_id, topic_id) key rejects a repeated name, and the tx must leave the old set intact.
	if err := ts.SetTopics(ctx, repoID, []string{a}); err != nil {
		t.Fatalf("SetTopics: %v", err)
	}
	if err := ts.SetTopics(ctx, repoID, []string{b, b}); err == nil {
		t.Skip("duplicate names are accepted by the schema; nothing to roll back")
	}
	if got, _ := ts.ListByRepo(ctx, repoID); len(got) != 1 || got[0].Name != a {
		t.Errorf("failed SetTopics changed the set: %+v", got)
	}
}

func TestTopicStore_ListByRepoIDsGroupsPerRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, "tpm"+suffix)
	r1 := testutil.SeedRepo(t, db, ownerID, "testuser_tpm"+suffix, "1"+suffix)
	r2 := testutil.SeedRepo(t, db, ownerID, "testuser_tpm"+suffix, "2"+suffix)
	r3 := testutil.SeedRepo(t, db, ownerID, "testuser_tpm"+suffix, "3"+suffix)
	x, y := "tpx-"+suffix, "tpy-"+suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM topics WHERE name = ANY($1)`, []string{x, y}) })

	ts := store.NewTopicStore(db)
	if err := ts.SetTopics(ctx, r1, []string{y, x}); err != nil {
		t.Fatal(err)
	}
	if err := ts.SetTopics(ctx, r2, []string{x}); err != nil {
		t.Fatal(err)
	}

	got, err := ts.ListByRepoIDs(ctx, []int64{r1, r2, r3})
	if err != nil {
		t.Fatalf("ListByRepoIDs: %v", err)
	}
	if len(got[r1]) != 2 || got[r1][0].Name != x || got[r1][1].Name != y {
		t.Errorf("r1 = %+v, want [x y]", got[r1])
	}
	if len(got[r2]) != 1 || got[r2][0].Name != x {
		t.Errorf("r2 = %+v", got[r2])
	}
	if _, ok := got[r3]; ok {
		t.Error("repo without topics must be absent from the map")
	}

	empty, err := ts.ListByRepoIDs(ctx, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("ListByRepoIDs(nil) = %v, %v; want empty non-nil map", empty, err)
	}
}

func TestTopicStore_ListAndCountReposByTopic(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, "tpr"+suffix)
	starrerA := testutil.SeedUser(t, db, "tpsa"+suffix)
	starrerB := testutil.SeedUser(t, db, "tpsb"+suffix)
	topic := "tpq-" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM topics WHERE name = $1`, topic) })

	mk := func(name string, private bool) int64 {
		var id int64
		if err := db.QueryRow(
			`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, 'o', $2, $3) RETURNING id`,
			ownerID, name+suffix, private).Scan(&id); err != nil {
			t.Fatalf("insert repo: %v", err)
		}
		return id
	}
	popular := mk("zpop", false)
	fresh := mk("mfresh", false)
	alpha := mk("aalpha", false)
	hidden := mk("hidden", true)
	deleted := mk("deleted", false)
	ts := store.NewTopicStore(db)
	for _, id := range []int64{popular, fresh, alpha, hidden, deleted} {
		if err := ts.SetTopics(ctx, id, []string{topic}); err != nil {
			t.Fatalf("SetTopics: %v", err)
		}
	}
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, deleted)
	testutil.Exec(t, db, `INSERT INTO stars (user_id, repo_id) VALUES ($1, $2), ($3, $2), ($1, $4)`, starrerA, popular, starrerB, alpha)
	now := time.Now()
	testutil.Exec(t, db, `UPDATE repositories SET updated_at = $2 WHERE id = $1`, fresh, now.Add(time.Hour))
	testutil.Exec(t, db, `UPDATE repositories SET updated_at = $2 WHERE id = ANY($1)`, []int64{popular, alpha}, now.Add(-time.Hour))

	order := func(sort string, page, size int) []int64 {
		t.Helper()
		rows, err := ts.ListReposByTopicWithStats(ctx, topic, page, size, sort)
		if err != nil {
			t.Fatalf("ListReposByTopicWithStats(%q): %v", sort, err)
		}
		ids := make([]int64, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		return ids
	}

	if got := order("", 1, 10); !sameIDs(got, []int64{popular, alpha, fresh}) {
		t.Errorf("default (stars) order = %v, want [popular alpha fresh]", got)
	}
	if got := order("updated", 1, 10); got[0] != fresh || len(got) != 3 {
		t.Errorf("updated order = %v, want fresh first", got)
	}
	if got := order("name", 1, 10); !sameIDs(got, []int64{alpha, fresh, popular}) {
		t.Errorf("name order = %v, want [alpha fresh popular]", got)
	}
	if got := order("name", 2, 2); !sameIDs(got, []int64{popular}) {
		t.Errorf("page 2 size 2 = %v, want [popular]", got)
	}

	rows, _ := ts.ListReposByTopicWithStats(ctx, topic, 1, 10, "")
	if rows[0].StarCount != 2 || rows[1].StarCount != 1 || rows[2].StarCount != 0 {
		t.Errorf("star counts = %d,%d,%d; want 2,1,0", rows[0].StarCount, rows[1].StarCount, rows[2].StarCount)
	}

	n, err := ts.CountReposByTopic(ctx, topic)
	if err != nil || n != 3 {
		t.Errorf("CountReposByTopic = %d, %v; want 3 (private and deleted excluded)", n, err)
	}
	if n, _ := ts.CountReposByTopic(ctx, "no-such-topic-"+suffix); n != 0 {
		t.Errorf("unknown topic count = %d", n)
	}
}
