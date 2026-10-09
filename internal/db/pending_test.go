package db_test

import (
	"context"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const userCodeThemesMigration = "101_user_code_themes"

func TestPending_MigratedSchema_ReportsNothing(t *testing.T) {
	fresh := testutil.OpenFreshTestDB(t)

	pending, err := db.Pending(context.Background(), fresh)

	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("want no pending migrations, got %v", pending)
	}
}

func TestPending_UnrecordedVersion_ReportsIt(t *testing.T) {
	fresh := testutil.OpenFreshTestDB(t)
	testutil.Exec(t, fresh, `DELETE FROM schema_migrations WHERE version = '`+userCodeThemesMigration+`'`)

	pending, err := db.Pending(context.Background(), fresh)

	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if !slices.Equal(pending, []string{userCodeThemesMigration}) {
		t.Errorf("want [%s], got %v", userCodeThemesMigration, pending)
	}
}

func TestPending_NoMigrationsTable_ReportsEverything(t *testing.T) {
	fresh := testutil.OpenFreshTestDB(t)
	var applied int
	if err := fresh.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count applied: %v", err)
	}
	testutil.Exec(t, fresh, `DROP TABLE schema_migrations`)

	pending, err := db.Pending(context.Background(), fresh)

	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != applied || !slices.IsSorted(pending) {
		t.Errorf("want all %d migrations in order, got %d: %v", applied, len(pending), pending)
	}
}

func TestMigrationKnown(t *testing.T) {
	for version, want := range map[string]bool{userCodeThemesMigration: true, "999_from_the_future": false, "": false} {
		got, err := db.MigrationKnown(version)
		if err != nil || got != want {
			t.Errorf("MigrationKnown(%q) = %v, %v; want %v", version, got, err, want)
		}
	}
}

func TestNewestApplied(t *testing.T) {
	fresh := testutil.OpenFreshTestDB(t)
	ctx := context.Background()

	pending, _ := db.Pending(ctx, fresh)
	if len(pending) != 0 {
		t.Fatalf("fresh schema has pending migrations: %v", pending)
	}
	testutil.Exec(t, fresh, `INSERT INTO schema_migrations (version) VALUES ('999_ahead')`)
	if got, err := db.NewestApplied(ctx, fresh); err != nil || got != "999_ahead" {
		t.Errorf("NewestApplied = %q, %v; want 999_ahead", got, err)
	}

	testutil.Exec(t, fresh, `DROP TABLE schema_migrations`)
	if got, err := db.NewestApplied(ctx, fresh); err != nil || got != "" {
		t.Errorf("without the table: NewestApplied = %q, %v; want empty", got, err)
	}
}
