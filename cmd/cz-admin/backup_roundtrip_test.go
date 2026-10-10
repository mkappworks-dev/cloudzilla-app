package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func countRows(t *testing.T, dsn, table string) int {
	t.Helper()
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// Runs the commands with the pg_dump and pg_restore on PATH, which must be major 18 or newer; skips without them.
func TestBackupThenRestore_WithTheRealPostgresTools(t *testing.T) {
	requirePgTools(t)
	srcDSN := scratchDatabase(t, true)
	src := newInstanceConfig(t, srcDSN)
	if _, stderr, err := runAdmin(t, seedCmd(), src.file(t), nil, smallWorld...); err != nil {
		t.Fatalf("seed: %v\n%s", err, stderr)
	}
	writeFile(t, src.hostKey, "HOST-KEY")
	archive := filepath.Join(t.TempDir(), "backup.tar")
	if _, stderr, err := runAdmin(t, backupCmd(), src.file(t), nil, "--output", archive); err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}

	dstDSN := scratchDatabase(t, false)
	dst := newInstanceConfig(t, dstDSN)
	stdout, stderr, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	assertOutput(t, "stdout", stdout, "repositories and database rows agree")

	for _, table := range []string{"users", "repositories", "issues", "pull_requests"} {
		want := countRows(t, srcDSN, table)
		if want == 0 {
			t.Errorf("the seed left %s empty; the round trip proves nothing for it", table)
		}
		if got := countRows(t, dstDSN, table); got != want {
			t.Errorf("%s: restored %d rows, backed up %d", table, got, want)
		}
	}
	if got, err := os.ReadFile(dst.hostKey); err != nil || string(got) != "HOST-KEY" {
		t.Errorf("host key = %q, %v; want the backed-up key", got, err)
	}
	owners, err := os.ReadDir(dst.reposRoot)
	if err != nil || len(owners) == 0 {
		t.Errorf("restored repos root holds %d entries, %v", len(owners), err)
	}
}
