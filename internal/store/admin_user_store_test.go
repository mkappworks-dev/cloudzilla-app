package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestUserStore_NeverZeroActiveSuperadmins(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	only := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))

	if _, err := users.Suspend(ctx, only); !errors.Is(err, store.ErrLastSuperadmin) {
		t.Errorf("suspend last: got %v", err)
	}
	if err := users.Demote(ctx, only); !errors.Is(err, store.ErrLastSuperadmin) {
		t.Errorf("demote last: got %v", err)
	}
	if _, err := users.DeleteWithOwnedRepos(ctx, only, nil); !errors.Is(err, store.ErrLastSuperadmin) {
		t.Errorf("delete last: got %v", err)
	}
	if last, err := users.IsLastActiveSuperadmin(ctx, only); err != nil || !last {
		t.Errorf("IsLastActiveSuperadmin = %v, %v; want true", last, err)
	}

	// A suspended superadmin doesn't count as one that remains.
	other := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, other)
	if err := users.Demote(ctx, only); !errors.Is(err, store.ErrLastSuperadmin) {
		t.Errorf("demote with only a suspended other: got %v", err)
	}

	// Removing someone who isn't an active superadmin never trips the guard.
	plain := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	if _, err := users.Suspend(ctx, plain); err != nil {
		t.Errorf("suspend plain user: %v", err)
	}
}

func TestUserStore_ConcurrentDemotionsLeaveOneSuperadmin(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	a := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	b := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))

	for round := 0; round < 10; round++ {
		testutil.Exec(t, db, `UPDATE users SET is_superadmin = TRUE, suspended_at = NULL WHERE id IN ($1, $2)`, a, b)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, id := range []int64{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i == 0 {
					errs[i] = users.Demote(ctx, id)
				} else {
					_, errs[i] = users.Suspend(ctx, id)
				}
			}()
		}
		wg.Wait()
		var active int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_superadmin AND suspended_at IS NULL AND id <> ghost_user_id()`).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active != 1 {
			t.Fatalf("round %d: %d active superadmins left (errs %v)", round, active, errs)
		}
		if !errors.Is(errs[0], store.ErrLastSuperadmin) && !errors.Is(errs[1], store.ErrLastSuperadmin) {
			t.Fatalf("round %d: neither removal was refused: %v", round, errs)
		}
	}
}

func TestUserStore_SuspendEndsSessionsForGood(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	id := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	before, err := users.SessionState(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	if changed, err := users.Suspend(ctx, id); err != nil || !changed {
		t.Fatalf("Suspend = %v, %v", changed, err)
	}
	if changed, err := users.Suspend(ctx, id); err != nil || changed {
		t.Errorf("second Suspend = %v, %v; want false", changed, err)
	}
	if _, err := users.SessionState(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SessionState while suspended: %v", err)
	}
	if changed, err := users.Unsuspend(ctx, id); err != nil || !changed {
		t.Fatalf("Unsuspend = %v, %v", changed, err)
	}
	after, err := users.SessionState(ctx, id)
	if err != nil || after.Version == before.Version {
		t.Errorf("session version %d → %d (%v); want it bumped", before.Version, after.Version, err)
	}
}

func TestUserStore_PromoteRefusesSuspended(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	id := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, id)
	if err := users.Promote(ctx, id); !errors.Is(err, store.ErrUserSuspended) {
		t.Errorf("Promote suspended: %v", err)
	}
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NULL WHERE id = $1`, id)
	if err := users.Promote(ctx, id); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if st, _ := users.SessionState(ctx, id); !st.IsSuperadmin {
		t.Error("not promoted")
	}
}

func TestUserStore_RevokeCredentialsKeepsDeployKeys(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	sfx := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, sfx)
	repoID := testutil.SeedRepo(t, db, id, "testuser_"+sfx, sfx)
	testutil.Exec(t, db, `INSERT INTO access_tokens (user_id, name, token_hash, last_eight) VALUES ($1, 't', $2, '12345678')`, id, "revoke_"+sfx)
	testutil.Exec(t, db, `INSERT INTO ssh_keys (user_id, title, public_key, fingerprint) VALUES ($1, 'k', 'ssh-ed25519 AAAA', $2)`, id, "fp_user_"+sfx)
	testutil.Exec(t, db, `INSERT INTO deploy_keys (repo_id, title, public_key, fingerprint) VALUES ($1, 'd', 'ssh-ed25519 BBBB', $2)`, repoID, "fp_deploy_"+sfx)
	before, _ := users.SessionState(ctx, id)

	got, err := users.RevokeCredentials(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got != (model.RevokedCredentials{AccessTokens: 1, SSHKeys: 1}) {
		t.Errorf("revoked %+v", got)
	}
	var deployKeys int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deploy_keys WHERE repo_id = $1`, repoID).Scan(&deployKeys); err != nil || deployKeys != 1 {
		t.Errorf("deploy keys left = %d (%v), want 1", deployKeys, err)
	}
	if after, _ := users.SessionState(ctx, id); after.Version != before.Version+1 {
		t.Errorf("session version %d → %d, want bumped", before.Version, after.Version)
	}
}

func TestUserStore_SoleOwnedOrgNames(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	sfx := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, "owner_"+sfx)
	coOwner := testutil.SeedUser(t, db, "co_"+sfx)
	seedOrg := func(name string, owners ...int64) {
		var orgID int64
		if err := db.QueryRowContext(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&orgID); err != nil {
			t.Fatal(err)
		}
		testutil.DeleteOrgOnCleanup(t, db, orgID)
		for _, u := range owners {
			testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, orgID, u)
		}
	}
	seedOrg("testorg_solo_"+sfx, owner)
	seedOrg("testorg_shared_"+sfx, owner, coOwner)

	got, err := store.NewUserStore(db).SoleOwnedOrgNames(ctx, owner)
	if err != nil || !slices.Equal(got, []string{"testorg_solo_" + sfx}) {
		t.Errorf("SoleOwnedOrgNames = %v, %v", got, err)
	}
}

func TestUserStore_ListForAdmin(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	admin := testutil.SeedSuperadmin(t, db, "a")
	plain := testutil.SeedUser(t, db, "b")
	suspended := testutil.SeedUser(t, db, "c")
	wild := testutil.SeedUser(t, db, "x_y")
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, suspended)
	testutil.Exec(t, db, `UPDATE users SET created_at = NOW() - make_interval(mins => id::int)`)

	ids := func(f model.AdminUserFilter, page, perPage int) ([]int64, int) {
		t.Helper()
		rows, total, err := users.ListForAdmin(ctx, f, page, perPage)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, r := range rows {
			out = append(out, r.User.ID)
		}
		return out, total
	}

	if got, total := ids(model.AdminUserFilter{}, 1, 50); total != 4 || !slices.Equal(got, []int64{admin, plain, suspended, wild}) {
		t.Errorf("all = %v (total %d); want newest first, ghost excluded", got, total)
	}
	if got, total := ids(model.AdminUserFilter{}, 2, 3); total != 4 || !slices.Equal(got, []int64{wild}) {
		t.Errorf("page 2 = %v (total %d)", got, total)
	}
	if got, _ := ids(model.AdminUserFilter{Role: model.AdminUserRoleSuperadmin}, 1, 50); !slices.Equal(got, []int64{admin}) {
		t.Errorf("superadmins = %v", got)
	}
	if got, _ := ids(model.AdminUserFilter{Status: model.AdminUserStatusSuspended}, 1, 50); !slices.Equal(got, []int64{suspended}) {
		t.Errorf("suspended = %v", got)
	}
	if got, _ := ids(model.AdminUserFilter{Status: model.AdminUserStatusActive}, 1, 50); slices.Contains(got, suspended) || len(got) != 3 {
		t.Errorf("active = %v", got)
	}
	if got, _ := ids(model.AdminUserFilter{Query: "TESTUSER_B"}, 1, 50); !slices.Equal(got, []int64{plain}) {
		t.Errorf("username prefix = %v", got)
	}
	if got, _ := ids(model.AdminUserFilter{Query: "testadmin_a@"}, 1, 50); !slices.Equal(got, []int64{admin}) {
		t.Errorf("email prefix = %v", got)
	}
	// "_" is a literal here, not LIKE's any-character.
	if got, _ := ids(model.AdminUserFilter{Query: "testuser_x_"}, 1, 50); !slices.Equal(got, []int64{wild}) {
		t.Errorf("literal underscore = %v", got)
	}
	if got, _ := ids(model.AdminUserFilter{Query: "%"}, 1, 50); len(got) != 0 {
		t.Errorf("percent = %v, want none", got)
	}
}
