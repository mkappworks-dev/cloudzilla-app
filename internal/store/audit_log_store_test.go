package store_test

// Each test gets a fresh schema: other packages' tests write audit rows concurrently.

import (
	"context"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestAuditLogStore_Count(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	actorID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	testutil.Exec(t, db, `INSERT INTO audit_log (actor_id, action, target_type, target_id) VALUES
		($1,   'login',             '',     NULL),
		($1,   'repo.create',       'repo', 42),
		(NULL, 'login',             '',     NULL),
		(NULL, 'repo.delete',       'repo', 43),
		(NULL, 'user.email.verify', 'user', 42)`, actorID)
	targetID := int64(42)
	s := store.NewAuditLogStore(db)

	for _, tc := range []struct {
		name   string
		filter model.AuditFilter
		want   int
	}{
		{"no filter", model.AuditFilter{}, 5},
		{"action", model.AuditFilter{Action: "login"}, 2},
		{"actor and action", model.AuditFilter{ActorID: &actorID, Action: "login"}, 1},
		{"target", model.AuditFilter{TargetType: "repo", TargetID: &targetID}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Count(context.Background(), tc.filter)
			if err != nil || got != tc.want {
				t.Errorf("Count = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestAuditLogStore_List_PagesNewestFirst(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	testutil.Exec(t, db, `INSERT INTO audit_log (actor_name, action, created_at) VALUES
		('oldest', 'login',       now() - interval '4 minutes'),
		('middle', 'login',       now() - interval '2 minutes'),
		('other',  'repo.create', now() - interval '3 minutes'),
		('newest', 'login',       now() - interval '1 minute')`)
	s := store.NewAuditLogStore(db)

	for page, want := range map[int][]string{1: {"newest", "middle"}, 2: {"oldest"}} {
		entries, err := s.List(context.Background(), model.AuditFilter{Action: "login"}, page, 2)
		if err != nil {
			t.Fatalf("List page %d: %v", page, err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, e.ActorName)
		}
		if !slices.Equal(got, want) {
			t.Errorf("List page %d = %v, want %v", page, got, want)
		}
	}
}
