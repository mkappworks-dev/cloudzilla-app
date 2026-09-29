package store_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedOrgNamed(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	testutil.Exec(t, db, `INSERT INTO organizations (name) VALUES ($1)`, name)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, name) })
}

func TestUserStore_OwnerNameTaken_AcrossUsersAndOrgsInAnyCase(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	seedOrgNamed(t, db, "nsorg_"+suffix)
	users := store.NewUserStore(db)

	for name, want := range map[string]bool{
		"TestUser_" + suffix: true,
		"NSORG_" + suffix:    true,
		"free_" + suffix:     false,
	} {
		got, err := users.OwnerNameTaken(context.Background(), name)
		if err != nil || got != want {
			t.Errorf("OwnerNameTaken(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
}

func TestUserInserts_RefuseAnOwnerNameTakenInAnyCase(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	users, sso := store.NewUserStore(db), store.NewSSOStore(db)

	inserts := map[string]func(username, email string) error{
		"Create": func(username, email string) error {
			return users.Create(ctx, &model.User{Username: username, Email: email, PasswordHash: "x"})
		},
		"CreateOAuthUser": func(username, email string) error {
			_, err := users.CreateOAuthUser(ctx, username, email, "github", email, "")
			return err
		},
		"CreateSuperadmin": func(username, email string) error {
			_, err := users.CreateSuperadmin(ctx, username, email, "x")
			return err
		},
		"ProvisionSSOUser": func(username, email string) error {
			_, err := sso.ProvisionSSOUser(ctx, username, email, "ldap", email)
			return err
		},
	}
	for label, insert := range inserts {
		suffix := testutil.UniqueSuffix(t)
		testutil.SeedUser(t, db, suffix)
		seedOrgNamed(t, db, "nsorg_"+suffix)
		for _, taken := range []string{"TestUser_" + suffix, "NSORG_" + suffix} {
			email := strings.ToLower(label) + "_" + testutil.UniqueSuffix(t) + "@test.invalid"
			t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE email = $1`, email) })

			if err := insert(taken, email); !errors.Is(err, store.ErrUsernameTaken) {
				t.Errorf("%s(%q): want ErrUsernameTaken, got %v", label, taken, err)
			}
			var n int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%s(%q): no user may be created, found %d", label, taken, n)
			}
		}
	}
}

func TestOrgStore_Create_RefusesAnOwnerNameTakenInAnyCase(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	seedOrgNamed(t, db, "nsorg_"+suffix)
	orgs := store.NewOrgStore(db)

	for _, taken := range []string{"TESTUSER_" + suffix, "NSOrg_" + suffix} {
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, taken) })

		if err := orgs.Create(context.Background(), &model.Organization{Name: taken}); !errors.Is(err, store.ErrOrgNameTaken) {
			t.Errorf("Create(%q): want ErrOrgNameTaken, got %v", taken, err)
		}
	}
}
