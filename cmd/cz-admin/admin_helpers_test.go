package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	fakeDumpBody = "FAKE-PGDUMP-BYTES"
	// Newer than any server the tests meet, so the client-version check passes.
	fakeToolVersion = "99.0"
)

// sharedDSN is the test database's DSN; the test is skipped without one.
func sharedDSN(t *testing.T) string {
	t.Helper()
	testutil.OpenTestDB(t)
	return os.Getenv("TEST_DATABASE_DSN")
}

// scratchDatabase creates a database next to the test one and returns its DSN. It is dropped when the test ends.
func scratchDatabase(t *testing.T, migrate bool) string {
	t.Helper()
	admin := testutil.OpenTestDB(t)
	name := "cz_admin_" + testutil.UniqueSuffix(t)
	testutil.Exec(t, admin, `CREATE DATABASE `+name)

	u, err := url.Parse(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil || u.Scheme == "" {
		t.Skip("TEST_DATABASE_DSN must be a postgres:// URL")
	}
	u.Path = "/" + name
	t.Cleanup(func() { testutil.Exec(t, admin, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`) })
	if migrate {
		d, err := sql.Open("pgx", u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Close() }()
		if err := db.Migrate(d); err != nil {
			t.Fatal(err)
		}
	}
	return u.String()
}

// instanceConfig is the slice of config.yaml the backup, restore and seed commands read.
type instanceConfig struct {
	dsn, reposRoot, hostKey, storageRoot string
	s3Bucket                             string
}

func newInstanceConfig(t *testing.T, dsn string) instanceConfig {
	t.Helper()
	dir := t.TempDir()
	return instanceConfig{
		dsn:         dsn,
		reposRoot:   filepath.Join(dir, "repos"),
		hostKey:     filepath.Join(dir, "host_key"),
		storageRoot: filepath.Join(dir, "storage"),
	}
}

// file writes the config and returns its path.
func (c instanceConfig) file(t *testing.T) string {
	t.Helper()
	storage := "  backend: local\n  local:\n    root: " + c.storageRoot + "\n"
	if c.s3Bucket != "" {
		storage = "  backend: s3\n  s3:\n    bucket: " + c.s3Bucket + "\n"
	}
	body := fmt.Sprintf("database:\n  dsn: %q\ngit:\n  repos_root: %q\n  ssh_host_key: %q\nstorage:\n%s",
		c.dsn, c.reposRoot, c.hostKey, storage)
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// runAdmin executes cmd against the config at cfgPath and returns what it printed.
func runAdmin(t *testing.T, cmd *cobra.Command, cfgPath string, stdin io.Reader, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	prev := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = prev })

	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if stdin != nil {
		cmd.SetIn(stdin)
	}
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

// writeTool writes an executable shell script; stand-ins for pg_dump and pg_restore keep these tests independent of a Postgres client install.
func writeTool(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeDump(t *testing.T) string {
	return writeTool(t, "pg_dump", `[ "$1" = "--version" ] && { echo "pg_dump (PostgreSQL) `+fakeToolVersion+`"; exit 0; }
printf '`+fakeDumpBody+`'`)
}

// fakeRestore drains the dump it is fed; the restored database stays empty, and the command migrates it afterwards.
func fakeRestore(t *testing.T) string {
	return writeTool(t, "pg_restore", `[ "$1" = "--version" ] && { echo "pg_restore (PostgreSQL) `+fakeToolVersion+`"; exit 0; }
cat > /dev/null`)
}

// requirePgTools skips unless real pg_dump and pg_restore are on PATH.
func requirePgTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH; skipping the real backup round trip", tool)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertOutput(t *testing.T, label, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s = %q; want it to contain %q", label, got, w)
		}
	}
}
