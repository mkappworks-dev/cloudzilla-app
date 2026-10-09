package main

// Integration tests for the gc and stats commands. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	read := func(r *os.File) chan string {
		ch := make(chan string, 1)
		go func() { b, _ := io.ReadAll(r); ch <- string(b) }()
		return ch
	}
	outCh, errCh := read(outR), read(errR)
	fn()
	_ = outW.Close()
	_ = errW.Close()
	return <-outCh, <-errCh
}

// useConfig points the command at an isolated schema and repos root through
// the CZ_* overrides, with a config file that sets nothing.
func useConfig(t *testing.T, dsn, reposRoot string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("server:\n  port: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := cfgFile
	cfgFile = file
	t.Cleanup(func() { cfgFile = old })
	t.Setenv("CZ_DATABASE_DSN", dsn)
	t.Setenv("CZ_GIT_REPOS_ROOT", reposRoot)
}

func freshSchema(t *testing.T) (*sql.DB, string) {
	t.Helper()
	d := testutil.OpenFreshTestDB(t)
	var schema string
	if err := d.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil || u.Scheme == "" {
		t.Skip("TEST_DATABASE_DSN must be a postgres:// URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return d, u.String()
}

func runCmd(t *testing.T, c *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c.SetArgs(args)
	c.SilenceUsage, c.SilenceErrors = true, true
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	stdout, stderr = captureOutput(t, func() { err = c.Execute() })
	return
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 1 << 20: "1.0 MiB", 3 << 30: "3.0 GiB", 1 << 40: "1.0 TiB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestResolveRepos(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alice/one.git", "alice/two.git", "alice/not-a-repo", "bob/three.git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "alice", "stray.git"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file-at-top"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("all", func(t *testing.T) {
		got, err := resolveRepos(root, "")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{filepath.Join(root, "alice", "one.git"), filepath.Join(root, "alice", "two.git"), filepath.Join(root, "bob", "three.git")}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("single", func(t *testing.T) {
		got, err := resolveRepos(root, "alice/one")
		if err != nil || len(got) != 1 || got[0] != filepath.Join(root, "alice", "one.git") {
			t.Errorf("got %v, %v", got, err)
		}
	})
	t.Run("invalid --repo", func(t *testing.T) {
		for arg, want := range map[string]string{
			"alice":       "owner/name form",
			"a/b/c":       "owner/name form",
			"../repo":     "invalid owner",
			"alice/..":    "invalid repo",
			"alice/a b":   "invalid repo",
			"/repo":       "invalid owner",
			"alice/repo/": "owner/name form",
		} {
			if _, err := resolveRepos(root, arg); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("--repo %q: err = %v, want one containing %q", arg, err, want)
			}
		}
	})
	t.Run("missing root", func(t *testing.T) {
		if _, err := resolveRepos(filepath.Join(root, "nope"), ""); err == nil || !strings.Contains(err.Error(), "read repos root") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("unreadable owner directory is skipped", func(t *testing.T) {
		locked := t.TempDir()
		_ = os.MkdirAll(filepath.Join(locked, "ok", "r.git"), 0o755)
		_ = os.MkdirAll(filepath.Join(locked, "locked", "r.git"), 0o755)
		if err := os.Chmod(filepath.Join(locked, "locked"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(locked, "locked"), 0o755) })
		if _, err := os.ReadDir(filepath.Join(locked, "locked")); err == nil {
			t.Skip("permissions are not enforced (running as root?)")
		}
		var got []string
		_, stderr := captureOutput(t, func() { got, _ = resolveRepos(locked, "") })
		if len(got) != 1 || !strings.Contains(stderr, "skipping") {
			t.Errorf("got %v, stderr %q", got, stderr)
		}
	})
}

// gcRepo makes a bare repository whose main ref keeps one commit while a
// second, unreferenced commit sits as a loose object.
func gcRepo(t *testing.T, dir string) (kept, dropped plumbing.Hash) {
	t.Helper()
	repo := testutil.InitBareRepo(t, dir)
	kept = testutil.WriteCommit(t, repo.Storer, "keep")
	dropped = testutil.WriteCommit(t, repo.Storer, "drop", kept)
	if err := repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", kept)); err != nil {
		t.Fatal(err)
	}
	return kept, dropped
}

func looseObject(dir string, h plumbing.Hash) string {
	return filepath.Join(dir, "objects", h.String()[:2], h.String()[2:])
}

func TestGCCommand(t *testing.T) {
	d, dsn := freshSchema(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, d, suffix)
	owner := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, d, ownerID, owner, suffix)
	root := t.TempDir()
	useConfig(t, dsn, root)

	gitDir := filepath.Join(root, owner, "testrepo_"+suffix+".git")
	_, dropped := gcRepo(t, gitDir)
	emptyDir := filepath.Join(root, owner, "empty.git")
	testutil.InitBareRepo(t, emptyDir)

	sizeOf := func() int64 {
		var n int64
		if err := d.QueryRow(`SELECT COALESCE(size_bytes, 0) FROM repositories WHERE id = $1`, repoID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("dry run reports and deletes nothing", func(t *testing.T) {
		stdout, _, err := runCmd(t, gcCmd(), "--dry-run", "--grace", "1ns")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"empty.git: skipped (no refs)", "pruned=1", "total: would prune 1 objects", "1 ok, 1 skipped, 0 failed"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
		if _, err := os.Stat(looseObject(gitDir, dropped)); err != nil {
			t.Errorf("dry run removed the unreferenced object: %v", err)
		}
		if sizeOf() != 0 {
			t.Error("dry run updated the repository size")
		}
	})
	t.Run("grace period spares recent objects", func(t *testing.T) {
		stdout, _, err := runCmd(t, gcCmd(), "--repo", owner+"/testrepo_"+suffix)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, "pruned=0") || !strings.Contains(stdout, "kept=1") {
			t.Errorf("stdout:\n%s", stdout)
		}
		if _, err := os.Stat(looseObject(gitDir, dropped)); err != nil {
			t.Errorf("object inside the grace period was removed: %v", err)
		}
	})
	t.Run("prunes and refreshes the stored size", func(t *testing.T) {
		time.Sleep(5 * time.Millisecond)
		stdout, _, err := runCmd(t, gcCmd(), "--grace", "1ms")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, "total: pruned 1 objects") {
			t.Errorf("stdout:\n%s", stdout)
		}
		if _, err := os.Stat(looseObject(gitDir, dropped)); !os.IsNotExist(err) {
			t.Errorf("unreferenced object still present: %v", err)
		}
		if sizeOf() <= 0 {
			t.Error("repository size not refreshed")
		}
	})
	t.Run("a repository that cannot be opened fails the run", func(t *testing.T) {
		broken := filepath.Join(root, owner, "broken.git")
		if err := os.MkdirAll(broken, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(broken) })
		stdout, stderr, err := runCmd(t, gcCmd())
		if err == nil || !strings.Contains(err.Error(), "1 repositories failed") {
			t.Errorf("err = %v", err)
		}
		if !strings.Contains(stderr, "broken.git") || !strings.Contains(stdout, "1 failed") {
			t.Errorf("stdout %q, stderr %q", stdout, stderr)
		}
	})
	t.Run("invalid --repo", func(t *testing.T) {
		if _, _, err := runCmd(t, gcCmd(), "--repo", "nonsense"); err == nil || !strings.Contains(err.Error(), "owner/name form") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("missing repos root", func(t *testing.T) {
		useConfig(t, dsn, filepath.Join(root, "nope"))
		if _, _, err := runCmd(t, gcCmd()); err == nil || !strings.Contains(err.Error(), "read repos root") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestGCCommand_UnreachableDatabaseOnlySkipsSizeUpdates(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, "alice", "r.git")
	_, dropped := gcRepo(t, gitDir)
	useConfig(t, "postgres://nobody:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", root)

	time.Sleep(5 * time.Millisecond)
	stdout, stderr, err := runCmd(t, gcCmd(), "--grace", "1ms")
	if err != nil {
		t.Fatalf("gc failed over an unreachable database: %v", err)
	}
	if !strings.Contains(stderr, "database unreachable") || !strings.Contains(stdout, "total: pruned 1 objects") {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
	if _, err := os.Stat(looseObject(gitDir, dropped)); !os.IsNotExist(err) {
		t.Error("object not pruned")
	}
}

func TestGCCommand_BadConfig(t *testing.T) {
	old := cfgFile
	cfgFile = filepath.Join(t.TempDir(), "absent.yaml")
	t.Cleanup(func() { cfgFile = old })
	if _, _, err := runCmd(t, gcCmd()); err == nil {
		t.Error("gc ran with a config file that does not exist")
	}
	if _, _, err := runCmd(t, statsBackfillCmd(), "--all"); err == nil {
		t.Error("backfill ran with a config file that does not exist")
	}
}

func TestStatsBackfill(t *testing.T) {
	d, dsn := freshSchema(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, d, suffix)
	owner := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, d, ownerID, owner, suffix)
	name := "testrepo_" + suffix
	root := t.TempDir()
	useConfig(t, dsn, root)

	weeks := func() int {
		var n int
		if err := d.QueryRow(`SELECT count(*) FROM contributor_week_stats WHERE repo_id = $1`, repoID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("flag validation", func(t *testing.T) {
		for _, args := range [][]string{{}, {"--all", "--repo", owner + "/" + name}} {
			if _, _, err := runCmd(t, statsBackfillCmd(), args...); err == nil || !strings.Contains(err.Error(), "exactly one of --repo or --all") {
				t.Errorf("args %v: err = %v", args, err)
			}
		}
		for _, bad := range []string{"noslash", "/name", "owner/"} {
			if _, _, err := runCmd(t, statsBackfillCmd(), "--repo", bad); err == nil || !strings.Contains(err.Error(), "owner/name") {
				t.Errorf("--repo %q: err = %v", bad, err)
			}
		}
		if _, _, err := runCmd(t, statsCmd(), "backfill", "--repo", "ghost/nothing"); err == nil {
			t.Error("backfill of an unknown repository succeeded")
		}
	})
	t.Run("repository row without a directory is skipped", func(t *testing.T) {
		stdout, _, err := runCmd(t, statsBackfillCmd(), "--repo", owner+"/"+name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, "skipped (no git repository on disk)") {
			t.Errorf("stdout:\n%s", stdout)
		}
	})

	repo := testutil.InitBareRepo(t, filepath.Join(root, owner, name+".git"))
	sig := object.Signature{Name: "Dev", Email: "testuser_" + suffix + "@test.invalid", When: time.Now()}
	first := testutil.WriteCommitBy(t, repo.Storer, sig, "one")
	head := testutil.WriteCommitBy(t, repo.Storer, sig, "two", first)
	stranger := object.Signature{Name: "Nobody", Email: "nobody@elsewhere.invalid", When: time.Now()}
	head = testutil.WriteCommitBy(t, repo.Storer, stranger, "three", head)
	if err := repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", head)); err != nil {
		t.Fatal(err)
	}

	t.Run("dry run reports drift and writes nothing", func(t *testing.T) {
		stdout, _, err := runCmd(t, statsBackfillCmd(), "--repo", owner+"/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"walked=3 known-user=2", "-> would set 2", "dry-run"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
		if weeks() != 0 {
			t.Error("dry run wrote stats")
		}
	})
	t.Run("apply rewrites the aggregates", func(t *testing.T) {
		stdout, _, err := runCmd(t, statsBackfillCmd(), "--all", "--apply")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, "-> set 2") || strings.Contains(stdout, "dry-run") {
			t.Errorf("stdout:\n%s", stdout)
		}
		var commits int
		if err := d.QueryRow(`SELECT COALESCE(sum(commits), 0) FROM contributor_week_stats WHERE repo_id = $1`, repoID).Scan(&commits); err != nil || commits != 2 {
			t.Errorf("stored commits = %d, %v; want 2", commits, err)
		}
	})
	t.Run("a repository that fails to walk fails the run", func(t *testing.T) {
		if err := os.Remove(looseObject(filepath.Join(root, owner, name+".git"), head)); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runCmd(t, statsBackfillCmd(), "--all")
		if err == nil || !strings.Contains(err.Error(), "1 repositories failed") {
			t.Errorf("err = %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
		if !strings.Contains(stderr, "backfill "+owner+"/"+name) {
			t.Errorf("stderr = %q", stderr)
		}
	})
}
