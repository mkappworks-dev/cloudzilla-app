package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/backup"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/seed"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newDatabase creates an empty database next to the test one and returns a
// handle on it with its DSN. It is dropped when the test ends.
func newDatabase(t *testing.T, label string) (*sql.DB, string) {
	t.Helper()
	admin := testutil.OpenTestDB(t)
	name := "cz_bk_" + label + "_" + testutil.UniqueSuffix(t)
	testutil.Exec(t, admin, `CREATE DATABASE `+name)

	u, err := url.Parse(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil || u.Scheme == "" {
		t.Skip("TEST_DATABASE_DSN must be a postgres:// URL")
	}
	u.Path = "/" + name
	d, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.Close()
		testutil.Exec(t, admin, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})
	return d, u.String()
}

func requirePgTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH; skipping the backup round trip", tool)
		}
	}
}

type instance struct {
	db          *sql.DB
	dsn         string
	reposRoot   string
	hostKey     string
	storageRoot string
}

// seededInstance is a migrated database filled by internal/seed, with the
// repositories, a host key and one stored avatar that go with it.
func seededInstance(t *testing.T) *instance {
	t.Helper()
	d, dsn := newDatabase(t, "src")
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	in := &instance{db: d, dsn: dsn, reposRoot: t.TempDir(), hostKey: filepath.Join(t.TempDir(), "host_key"), storageRoot: t.TempDir()}
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: "backup-test-secret-0123456789abcdef", JWTExpiry: time.Hour, CookieName: "cz_backup_test"},
		Git:  config.GitConfig{ReposRoot: in.reposRoot},
	}
	_, err := seed.Run(context.Background(), service.New(store.New(d), cfg), in.reposRoot,
		seed.Options{Users: 6, Orgs: 2, Repos: 5, Seed: 7, Now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(in.hostKey, []byte("-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	avatar := filepath.Join(in.storageRoot, "avatars", "user", "1", "abc.png")
	if err := os.MkdirAll(filepath.Dir(avatar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(avatar, []byte("png-bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	return in
}

func (in *instance) backup(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "backup.tar")
	_, err := backup.Create(context.Background(), out, nil, backup.CreateOptions{
		DB: in.db, DSN: in.dsn, ReposRoot: in.reposRoot, HostKeyPath: in.hostKey, StorageRoot: in.storageRoot, Version: "test",
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	return out
}

type target struct {
	db          *sql.DB
	opts        backup.RestoreOptions
	reposRoot   string
	hostKey     string
	storageRoot string
}

func newTarget(t *testing.T) *target {
	t.Helper()
	d, dsn := newDatabase(t, "dst")
	tg := &target{db: d, reposRoot: filepath.Join(t.TempDir(), "repos"), hostKey: filepath.Join(t.TempDir(), "host_key"), storageRoot: filepath.Join(t.TempDir(), "storage")}
	stores := store.New(d)
	tg.opts = backup.RestoreOptions{
		DB: d, DSN: dsn, ReposRoot: tg.reposRoot, HostKeyPath: tg.hostKey, StorageRoot: tg.storageRoot,
		ListRepos: func(ctx context.Context) ([]backup.RepoRef, error) {
			repos, err := stores.Repo.ListAll(ctx)
			var refs []backup.RepoRef
			for _, r := range repos {
				refs = append(refs, backup.RepoRef{Owner: r.OwnerName, Name: r.Name})
			}
			return refs, err
		},
	}
	return tg
}

func tableCounts(t *testing.T, d *sql.DB) map[string]int {
	t.Helper()
	rows, err := d.Query(`SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	counts := map[string]int{}
	for _, n := range names {
		var c int
		if err := d.QueryRow(`SELECT count(*) FROM "` + n + `"`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		counts[n] = c
	}
	return counts
}

func gitDirs(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err == nil && e.IsDir() && strings.HasSuffix(e.Name(), ".git") {
			rel, _ := filepath.Rel(root, p)
			dirs = append(dirs, rel)
			return filepath.SkipDir
		}
		return nil
	})
	slices.Sort(dirs)
	return dirs
}

func refsOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	refs := map[string]string{}
	it, _ := repo.References()
	_ = it.ForEach(func(r *plumbing.Reference) error {
		refs[r.Name().String()] = r.Hash().String() + r.Target().String()
		return nil
	})
	return refs
}

func TestBackupRestore_RoundTrip(t *testing.T) {
	requirePgTools(t)
	in := seededInstance(t)
	archive := in.backup(t)

	if info, err := os.Stat(archive); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode = %v, %v; want 0600", info, err)
	}

	tg := newTarget(t)
	rep, err := backup.Restore(context.Background(), archive, nil, tg.opts)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	want, got := tableCounts(t, in.db), tableCounts(t, tg.db)
	if len(want) < 20 {
		t.Fatalf("only %d tables compared; the schema did not load", len(want))
	}
	for table, n := range want {
		if got[table] != n {
			t.Errorf("table %s: %d rows restored, %d backed up", table, got[table], n)
		}
	}
	if pending, _ := db.Pending(context.Background(), tg.db); len(pending) != 0 {
		t.Errorf("migrations still pending after restore: %v", pending)
	}

	dirs := gitDirs(t, in.reposRoot)
	if len(dirs) == 0 {
		t.Fatal("seed made no repositories")
	}
	if !slices.Equal(dirs, gitDirs(t, tg.reposRoot)) {
		t.Fatalf("repository directories differ: %v vs %v", dirs, gitDirs(t, tg.reposRoot))
	}
	for _, d := range dirs {
		src, dst := filepath.Join(in.reposRoot, d), filepath.Join(tg.reposRoot, d)
		if a, b := refsOf(t, src), refsOf(t, dst); len(a) == 0 || !mapsEqual(a, b) {
			t.Errorf("%s: refs differ: %v vs %v", d, a, b)
		}
		repo, _ := gogit.PlainOpen(dst)
		head, err := repo.Head()
		if err != nil {
			continue // an empty repository has no HEAD commit
		}
		log, err := repo.Log(&gogit.LogOptions{From: head.Hash()})
		if err != nil {
			t.Errorf("%s: log: %v", d, err)
			continue
		}
		if err := log.ForEach(func(c *object.Commit) error { return nil }); err != nil {
			t.Errorf("%s: history walk: %v", d, err)
		}
	}

	key, _ := os.ReadFile(tg.hostKey)
	wantKey, _ := os.ReadFile(in.hostKey)
	if !bytes.Equal(key, wantKey) {
		t.Error("host key differs")
	}
	if info, _ := os.Stat(tg.hostKey); info.Mode().Perm() != 0o600 {
		t.Errorf("host key mode = %v, want 0600", info.Mode().Perm())
	}
	if b, _ := os.ReadFile(filepath.Join(tg.storageRoot, "avatars", "user", "1", "abc.png")); string(b) != "png-bytes" {
		t.Errorf("stored avatar = %q", b)
	}
	if len(rep.MissingDirs)+len(rep.OrphanDirs) != 0 {
		t.Errorf("reconciliation reported %v / %v", rep.MissingDirs, rep.OrphanDirs)
	}
}

func TestBackupRestore_ThroughStdoutAndStdin(t *testing.T) {
	requirePgTools(t)
	in := seededInstance(t)

	var archive bytes.Buffer
	_, err := backup.Create(context.Background(), "-", &archive, backup.CreateOptions{
		DB: in.db, DSN: in.dsn, ReposRoot: in.reposRoot, HostKeyPath: in.hostKey, Version: "test",
	})
	if err != nil {
		t.Fatalf("backup to stdout: %v", err)
	}

	tg := newTarget(t)
	if _, err := backup.Restore(context.Background(), "-", &archive, tg.opts); err != nil {
		t.Fatalf("restore from stdin: %v", err)
	}
	if want, got := tableCounts(t, in.db), tableCounts(t, tg.db); !slices.Equal(mapKeys(want), mapKeys(got)) {
		t.Errorf("tables differ: %v vs %v", mapKeys(want), mapKeys(got))
	}
	if _, err := os.Stat(tg.storageRoot); err == nil {
		t.Error("storage root created though the backup had no storage section")
	}
}

func mapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestRestore_ReportsRowsAndDirectoriesThatDisagree(t *testing.T) {
	requirePgTools(t)
	in := seededInstance(t)
	dirs := gitDirs(t, in.reposRoot)
	if len(dirs) < 2 {
		t.Fatal("seed made fewer than two repositories")
	}
	// Row without a directory, and a directory whose row was soft-deleted.
	if err := os.RemoveAll(filepath.Join(in.reposRoot, dirs[0])); err != nil {
		t.Fatal(err)
	}
	owner, name := filepath.Split(dirs[1])
	testutil.Exec(t, in.db, `UPDATE repositories SET deleted_at = now() WHERE owner_name = $1 AND name = $2`,
		filepath.Clean(owner), strings.TrimSuffix(name, ".git"))
	archive := in.backup(t)

	tg := newTarget(t)
	rep, err := backup.Restore(context.Background(), archive, nil, tg.opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSuffix(dirs[0], ".git"); !slices.Contains(rep.MissingDirs, want) {
		t.Errorf("missing dirs = %v, want %s", rep.MissingDirs, want)
	}
	if !slices.Contains(rep.OrphanDirs, dirs[1]) {
		t.Errorf("orphan dirs = %v, want %s", rep.OrphanDirs, dirs[1])
	}
}

func TestRestore_RefusesWithoutWritingAnything(t *testing.T) {
	requirePgTools(t)
	in := seededInstance(t)
	archive := in.backup(t)

	untouched := func(t *testing.T, tg *target) {
		t.Helper()
		var tables int
		_ = tg.db.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname = 'public'`).Scan(&tables)
		if tables != 0 {
			t.Errorf("refused restore left %d tables behind", tables)
		}
		if _, err := os.Stat(tg.hostKey); err == nil {
			t.Error("refused restore wrote a host key")
		}
	}
	entries := func(dir string) int { e, _ := os.ReadDir(dir); return len(e) }

	t.Run("non-empty database", func(t *testing.T) {
		tg := newTarget(t)
		tg.opts.DB, tg.opts.DSN = in.db, in.dsn
		_, err := backup.Restore(context.Background(), archive, nil, tg.opts)
		wantErr(t, err, "schema_migrations")
		if entries(tg.reposRoot) != 0 {
			t.Error("repos root was written")
		}
	})
	t.Run("non-empty repos root", func(t *testing.T) {
		tg := newTarget(t)
		if err := os.MkdirAll(tg.reposRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(tg.reposRoot, "stray"), []byte("x"), 0o644)
		_, err := backup.Restore(context.Background(), archive, nil, tg.opts)
		wantErr(t, err, "not empty")
		untouched(t, tg)
	})
	t.Run("different host key", func(t *testing.T) {
		tg := newTarget(t)
		_ = os.WriteFile(tg.hostKey, []byte("another key"), 0o600)
		_, err := backup.Restore(context.Background(), archive, nil, tg.opts)
		wantErr(t, err, "--replace-host-key")
		if entries(tg.reposRoot) != 0 {
			t.Error("repos root was written")
		}
		var tables int
		_ = tg.db.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname = 'public'`).Scan(&tables)
		if tables != 0 {
			t.Errorf("refused restore left %d tables behind", tables)
		}

		tg.opts.ReplaceHostKey = true
		if _, err := backup.Restore(context.Background(), archive, nil, tg.opts); err != nil {
			t.Fatalf("with --replace-host-key: %v", err)
		}
		want, _ := os.ReadFile(in.hostKey)
		if got, _ := os.ReadFile(tg.hostKey); !bytes.Equal(got, want) {
			t.Error("host key not replaced")
		}
	})

	crafted := []struct {
		name    string
		archive func(t *testing.T) string
		want    string
	}{
		{"unknown format version", func(t *testing.T) string {
			return craft(t, backup.Manifest{FormatVersion: 99}, nil)
		}, "format version 99"},
		{"migration this binary lacks", func(t *testing.T) string {
			return craft(t, backup.Manifest{FormatVersion: backup.FormatVersion, Migration: "999_from_the_future", PgDumpVersion: "18.0",
				Sections: map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 4}}},
				[]tar.Header{{Name: "database.pgdump", Typeflag: tar.TypeReg, Size: 4}})
		}, "999_from_the_future"},
		{"path traversal", func(t *testing.T) string { return craftWithEntry(t, "git-repos/../../escape", tar.TypeReg) }, "unsafe"},
		{"absolute path", func(t *testing.T) string { return craftWithEntry(t, "/etc/cron.d/x", tar.TypeReg) }, "unsafe"},
		{"symlink", func(t *testing.T) string { return craftWithEntry(t, "git-repos/link", tar.TypeSymlink) }, "unsupported type"},
	}
	for _, c := range crafted {
		t.Run(c.name, func(t *testing.T) {
			tg := newTarget(t)
			_, err := backup.Restore(context.Background(), c.archive(t), nil, tg.opts)
			wantErr(t, err, c.want)
			if entries(tg.reposRoot) != 0 {
				t.Error("repos root was written")
			}
			untouched(t, tg)
		})
	}
}

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("err = %v, want one containing %q", err, substr)
	}
}

// craft writes a tar with a manifest followed by empty-bodied entries.
func craft(t *testing.T, m backup.Manifest, entries []tar.Header) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crafted.tar")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tw := tar.NewWriter(f)
	body, _ := json.Marshal(m)
	_ = tw.WriteHeader(&tar.Header{Name: "cloudzilla-backup.json", Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0o600})
	_, _ = tw.Write(body)
	for _, h := range entries {
		h := h
		h.Mode = 0o600
		if h.Typeflag == tar.TypeSymlink {
			h.Linkname = "/etc"
		}
		_ = tw.WriteHeader(&h)
		if h.Size > 0 {
			_, _ = tw.Write(make([]byte, h.Size))
		}
	}
	_ = tw.Close()
	return path
}

func craftWithEntry(t *testing.T, name string, typ byte) string {
	t.Helper()
	m := backup.Manifest{FormatVersion: backup.FormatVersion, Migration: "001_init", PgDumpVersion: "18.0"}
	return craft(t, m, []tar.Header{{Name: name, Typeflag: typ}})
}

func TestCreate_FailsBeforeWritingWhenPgDumpIsMissingOrTooOld(t *testing.T) {
	admin := testutil.OpenTestDB(t)
	dsn := os.Getenv("TEST_DATABASE_DSN")
	var num string
	_ = admin.QueryRow(`SHOW server_version_num`).Scan(&num)
	major := num[:len(num)-4]

	old := filepath.Join(t.TempDir(), "pg_dump")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho 'pg_dump (PostgreSQL) 9.6.24'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, tool := range map[string]string{"too old": old, "missing": filepath.Join(t.TempDir(), "nope")} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "backup.tar")
			_, err := backup.Create(context.Background(), out, nil, backup.CreateOptions{
				DB: admin, DSN: dsn, PgDump: tool, ReposRoot: t.TempDir(), Version: "test",
			})
			wantErr(t, err, "postgresql"+major+"-client")
			if left, _ := os.ReadDir(dir); len(left) != 0 {
				t.Errorf("left %v behind", left)
			}
		})
	}
}
