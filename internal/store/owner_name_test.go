package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func wantUniqueViolation(t *testing.T, what string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("%s: err = %v, want a unique violation", what, err)
	}
}

// The service checks run before the insert; this is the backstop for two
// creates racing between check and insert.
func TestOwnerNames_UsersAndOrgsCannotShareAName(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	orgs := store.NewOrgStore(db)

	taken := &model.Organization{Name: "testuser_" + suffix}
	err := orgs.Create(ctx, taken)
	if taken.ID != 0 {
		testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, taken.ID)
	}
	wantUniqueViolation(t, "org with a user's name", err)

	org := &model.Organization{Name: "testorg_" + suffix}
	if err := orgs.Create(ctx, org); err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, org.ID) })
	var id int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		org.Name, "owner_name_"+suffix+"@test.invalid",
	).Scan(&id)
	if id != 0 {
		testutil.DeleteUsers(t, db, id)
	}
	wantUniqueViolation(t, "user with an org's name", err)
}
