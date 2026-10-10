package main

// Tests for the backup and restore commands. They need TEST_DATABASE_DSN and skip otherwise.
// Stand-in pg_dump and pg_restore scripts stand for the real tools; the one test that uses
// the real ones skips unless they are on PATH (see docs/testing.md).

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/backup"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// populate gives the instance one repository, a host key and one stored file.
func populate(t *testing.T, c instanceConfig) {
	t.Helper()
	testutil.InitBareRepo(t, filepath.Join(c.reposRoot, "alice", "proj.git"))
	writeFile(t, c.hostKey, "HOST-KEY")
	writeFile(t, filepath.Join(c.storageRoot, "avatars", "a.png"), "PNG")
}

func archiveNames(t *testing.T, archive io.Reader) []string {
	t.Helper()
	var names []string
	tr := tar.NewReader(archive)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
}

func hasPrefixed(names []string, prefix string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, prefix) })
}

func TestBackupCmd_WritesTheArchiveAndSummarizesIt(t *testing.T) {
	c := newInstanceConfig(t, sharedDSN(t))
	populate(t, c)
	out := filepath.Join(t.TempDir(), "backup.tar")

	stdout, stderr, err := runAdmin(t, backupCmd(), c.file(t), nil, "--output", out, "--pg-dump", fakeDump(t))
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q; want nothing, the summary goes to stderr", stdout)
	}
	assertOutput(t, "stderr", stderr, "backup written to "+out, "pg_dump "+fakeToolVersion,
		backup.SectionDatabase, backup.SectionGitRepos, backup.SectionStorage, backup.SectionHostKey)

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	names := archiveNames(t, f)
	for _, section := range []string{"cloudzilla-backup.json", "database.pgdump", backup.SectionGitRepos + "/", backup.SectionStorage + "/", backup.SectionHostKey} {
		if !hasPrefixed(names, section) {
			t.Errorf("archive entries %v; want one under %q", names, section)
		}
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("archive mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestBackupCmd_StreamsTheArchiveToStdout(t *testing.T) {
	c := newInstanceConfig(t, sharedDSN(t))
	populate(t, c)

	stdout, stderr, err := runAdmin(t, backupCmd(), c.file(t), nil, "--output", "-", "--pg-dump", fakeDump(t))
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}
	if names := archiveNames(t, strings.NewReader(stdout)); !hasPrefixed(names, "database.pgdump") {
		t.Errorf("stdout archive entries = %v; want the database dump", names)
	}
	assertOutput(t, "stderr", stderr, "backup written to -")
}

func TestBackupCmd_SaysWhichBucketItLeavesOut(t *testing.T) {
	c := newInstanceConfig(t, sharedDSN(t))
	c.s3Bucket = "cz-media"
	populate(t, c)

	_, stderr, err := runAdmin(t, backupCmd(), c.file(t), nil, "--output", filepath.Join(t.TempDir(), "b.tar"), "--pg-dump", fakeDump(t))
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}
	assertOutput(t, "stderr", stderr, `s3 bucket "cz-media" is not in the archive`)
	if strings.Contains(stderr, backup.SectionStorage+" ") {
		t.Errorf("stderr = %q; want no local storage section with an s3 backend", stderr)
	}
}

func TestBackupCmd_Failures(t *testing.T) {
	failingDump := writeTool(t, "pg_dump", `[ "$1" = "--version" ] && { echo "pg_dump (PostgreSQL) `+fakeToolVersion+`"; exit 0; }
echo "boom" >&2; exit 3`)

	cases := []struct {
		name    string
		dsn     string
		args    []string
		wantErr string
	}{
		{"needs --output", sharedDSN(t), nil, `required flag(s) "output" not set`},
		{"a database that is down", "postgres://nobody:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", []string{"--output", "-"}, ""},
		{"a pg_dump that fails", sharedDSN(t), []string{"--output", "-", "--pg-dump", failingDump}, "boom"},
		{"a pg_dump that is not installed", sharedDSN(t), []string{"--output", "-", "--pg-dump", "/no/such/pg_dump"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newInstanceConfig(t, tc.dsn)
			_, _, err := runAdmin(t, backupCmd(), c.file(t), nil, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v; want one containing %q", err, tc.wantErr)
			}
		})
	}

	t.Run("a config that does not exist", func(t *testing.T) {
		if _, _, err := runAdmin(t, backupCmd(), filepath.Join(t.TempDir(), "missing.yaml"), nil, "--output", "-"); err == nil {
			t.Error("want an error")
		}
	})
}

// takeBackup backs up the shared test database plus the instance's files with the stand-in pg_dump.
func takeBackup(t *testing.T, c instanceConfig) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "backup.tar")
	if _, stderr, err := runAdmin(t, backupCmd(), c.file(t), nil, "--output", out, "--pg-dump", fakeDump(t)); err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}
	return out
}

func TestRestoreCmd_RebuildsAnEmptyInstanceAndReportsWhatDoesNotMatch(t *testing.T) {
	src := newInstanceConfig(t, sharedDSN(t))
	populate(t, src)
	archive := takeBackup(t, src)

	dst := newInstanceConfig(t, scratchDatabase(t, false))
	stdout, stderr, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive, "--pg-restore", fakeRestore(t))
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	assertOutput(t, "stdout", stdout, "restored a backup taken", "Cloudzilla dev", "warning: directory alice/proj.git has no database row")
	assertOutput(t, "stderr", stderr, "restoring database", "applying newer migrations")
	if strings.Contains(stdout, "repositories and database rows agree") {
		t.Errorf("stdout = %q; want the orphan directory flagged, not an all-clear", stdout)
	}

	if _, err := os.Stat(filepath.Join(dst.reposRoot, "alice", "proj.git", "HEAD")); err != nil {
		t.Errorf("repository not restored: %v", err)
	}
	if got, err := os.ReadFile(dst.hostKey); err != nil || string(got) != "HOST-KEY" {
		t.Errorf("host key = %q, %v; want the backed-up key", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dst.storageRoot, "avatars", "a.png")); err != nil || string(got) != "PNG" {
		t.Errorf("storage file = %q, %v; want the backed-up file", got, err)
	}
}

func TestRestoreCmd_SaysSoWhenRepositoriesAndRowsAgree(t *testing.T) {
	src := newInstanceConfig(t, sharedDSN(t))
	writeFile(t, src.hostKey, "HOST-KEY")
	archive := takeBackup(t, src)

	dst := newInstanceConfig(t, scratchDatabase(t, false))
	stdout, stderr, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive, "--pg-restore", fakeRestore(t))
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	assertOutput(t, "stdout", stdout, "repositories and database rows agree")
}

func TestRestoreCmd_ReadsTheArchiveFromStdinAndWarnsAboutObjectStorage(t *testing.T) {
	src := newInstanceConfig(t, sharedDSN(t))
	src.s3Bucket = "cz-media"
	writeFile(t, src.hostKey, "HOST-KEY")
	archive, err := os.ReadFile(takeBackup(t, src))
	if err != nil {
		t.Fatal(err)
	}

	dst := newInstanceConfig(t, scratchDatabase(t, false))
	stdout, stderr, err := runAdmin(t, restoreCmd(), dst.file(t), bytes.NewReader(archive), "--input", "-", "--pg-restore", fakeRestore(t))
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	assertOutput(t, "stdout", stdout, `warning: the backup used s3 object storage (bucket "cz-media")`)
}

func TestRestoreCmd_RefusesWhatItWouldOverwrite(t *testing.T) {
	src := newInstanceConfig(t, sharedDSN(t))
	writeFile(t, src.hostKey, "HOST-KEY")
	archive := takeBackup(t, src)
	restoreTool := fakeRestore(t)

	t.Run("a database that already has tables", func(t *testing.T) {
		dst := newInstanceConfig(t, sharedDSN(t))
		_, _, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive, "--pg-restore", restoreTool)
		if err == nil || !strings.Contains(err.Error(), "new, empty database") {
			t.Errorf("err = %v; want a refusal to restore into a database that has a schema", err)
		}
	})

	t.Run("a repos root that is not empty", func(t *testing.T) {
		dst := newInstanceConfig(t, scratchDatabase(t, false))
		writeFile(t, filepath.Join(dst.reposRoot, "stray"), "x")
		_, _, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive, "--pg-restore", restoreTool)
		if err == nil || !strings.Contains(err.Error(), "not empty") {
			t.Errorf("err = %v; want a refusal naming the repos root", err)
		}
	})

	t.Run("a different host key unless --replace-host-key", func(t *testing.T) {
		dst := newInstanceConfig(t, scratchDatabase(t, false))
		writeFile(t, dst.hostKey, "OTHER-KEY")
		if _, _, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive, "--pg-restore", restoreTool); err == nil {
			t.Fatal("want a refusal to replace the existing host key")
		}
		if got, _ := os.ReadFile(dst.hostKey); string(got) != "OTHER-KEY" {
			t.Fatalf("host key = %q; the refused restore must not touch it", got)
		}

		if _, stderr, err := runAdmin(t, restoreCmd(), dst.file(t), nil, "--input", archive, "--pg-restore", restoreTool, "--replace-host-key"); err != nil {
			t.Fatalf("restore with --replace-host-key: %v\n%s", err, stderr)
		}
		if got, _ := os.ReadFile(dst.hostKey); string(got) != "HOST-KEY" {
			t.Errorf("host key = %q; want the backed-up key", got)
		}
	})
}

func TestRestoreCmd_Failures(t *testing.T) {
	t.Run("needs --input", func(t *testing.T) {
		c := newInstanceConfig(t, sharedDSN(t))
		_, _, err := runAdmin(t, restoreCmd(), c.file(t), nil)
		if err == nil || !strings.Contains(err.Error(), `required flag(s) "input" not set`) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("a config that does not exist", func(t *testing.T) {
		if _, _, err := runAdmin(t, restoreCmd(), filepath.Join(t.TempDir(), "missing.yaml"), nil, "--input", "-"); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("a database that is down", func(t *testing.T) {
		c := newInstanceConfig(t, "postgres://nobody:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
		if _, _, err := runAdmin(t, restoreCmd(), c.file(t), nil, "--input", "-"); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("an archive that does not exist", func(t *testing.T) {
		c := newInstanceConfig(t, sharedDSN(t))
		if _, _, err := runAdmin(t, restoreCmd(), c.file(t), nil, "--input", filepath.Join(t.TempDir(), "missing.tar")); err == nil {
			t.Error("want an error")
		}
	})
}
