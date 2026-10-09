package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/backup"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	fakeDumpBody = "FAKE-PGDUMP-BYTES"
	fakeVersion  = "99.0"
)

// writeTool writes an executable shell script; stand-ins for pg_dump and
// pg_restore keep these tests independent of a Postgres client install.
func writeTool(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeDump(t *testing.T) string {
	return writeTool(t, "pg_dump", `[ "$1" = "--version" ] && { echo "pg_dump (PostgreSQL) `+fakeVersion+`"; exit 0; }
printf '`+fakeDumpBody+`'`)
}

// fakeRestore copies the dump it is fed to the returned path and its argv to path+".args".
func fakeRestore(t *testing.T) (tool, received string) {
	received = filepath.Join(t.TempDir(), "received")
	return writeTool(t, "pg_restore", `[ "$1" = "--version" ] && { echo "pg_restore (PostgreSQL) `+fakeVersion+`"; exit 0; }
echo "$@" > "`+received+`.args"
cat > "`+received+`"`), received
}

func migratedDatabase(t *testing.T, label string) (*sql.DB, string) {
	t.Helper()
	d, dsn := newDatabase(t, label)
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	return d, dsn
}

// fixture is a migrated database with a repos root, storage root and host key on disk.
type fixture struct {
	db        *sql.DB
	dsn       string
	opts      backup.CreateOptions
	reposRoot string
	storage   string
	hostKey   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	d, dsn := migratedDatabase(t, "fk")
	f := &fixture{db: d, dsn: dsn, reposRoot: filepath.Join(t.TempDir(), "repos"), storage: filepath.Join(t.TempDir(), "storage"), hostKey: filepath.Join(t.TempDir(), "host_key")}

	testutil.InitBareRepo(t, filepath.Join(f.reposRoot, "alice", "proj.git"))
	f.writeFile(t, filepath.Join(f.reposRoot, "alice", "proj.git", "refs", "heads", "main"), "0123456789012345678901234567890123456789\n")
	f.writeFile(t, filepath.Join(f.reposRoot, "alice", "proj.git", "refs", "heads", "main.lock"), "half-written")
	f.writeFile(t, filepath.Join(f.reposRoot, "alice", "proj.git", "objects", "pack", "tmp_pack_abc"), "temp pack")
	f.writeFile(t, filepath.Join(f.reposRoot, ".import-tmp", "junk"), "junk")
	f.writeFile(t, filepath.Join(f.reposRoot, ".readyz-probe"), "probe")
	f.writeFile(t, filepath.Join(f.reposRoot, "alice", "notes.txt"), "plain file")
	if err := os.Symlink("/etc", filepath.Join(f.reposRoot, "alice", "link")); err != nil {
		t.Fatal(err)
	}
	f.writeFile(t, filepath.Join(f.storage, "avatars", "user", "1", "a.png"), "png")
	f.writeFile(t, filepath.Join(f.storage, ".tmp-upload"), "partial")
	f.writeFile(t, f.hostKey, "HOST-KEY\n")

	f.opts = backup.CreateOptions{DB: d, DSN: dsn, PgDump: fakeDump(t), ReposRoot: f.reposRoot, HostKeyPath: f.hostKey, StorageRoot: f.storage, Version: "vtest"}
	return f
}

func (f *fixture) writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) create(t *testing.T) (string, *backup.Manifest) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "b.tar")
	m, err := backup.Create(context.Background(), out, nil, f.opts)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return out, m
}

func readTar(t *testing.T, path string) (names []string, bodies map[string]string, types map[string]byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	bodies, types = map[string]string{}, map[string]byte{}
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		names = append(names, h.Name)
		bodies[h.Name], types[h.Name] = string(b), h.Typeflag
	}
}

func TestCreate_ArchivesEverySectionAndSkipsHalfWrittenState(t *testing.T) {
	f := newFixture(t)
	out, m := f.create(t)

	names, bodies, _ := readTar(t, out)
	if names[0] != "cloudzilla-backup.json" || names[1] != "database.pgdump" {
		t.Fatalf("entry order starts %v", names[:2])
	}
	if bodies["database.pgdump"] != fakeDumpBody {
		t.Errorf("dump = %q", bodies["database.pgdump"])
	}
	if bodies["ssh_host_key"] != "HOST-KEY\n" {
		t.Errorf("host key = %q", bodies["ssh_host_key"])
	}
	if bodies["git-repos/alice/notes.txt"] != "plain file" || bodies["storage/avatars/user/1/a.png"] != "png" {
		t.Error("repo or storage file missing from the archive")
	}
	for _, n := range names {
		for _, banned := range []string{".lock", "tmp_pack", ".import-tmp", ".readyz-", "link", ".tmp-upload"} {
			if strings.Contains(n, banned) {
				t.Errorf("archive holds %q", n)
			}
		}
	}

	var onDisk backup.Manifest
	if err := json.Unmarshal([]byte(bodies["cloudzilla-backup.json"]), &onDisk); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if onDisk.Migration == "" || onDisk.PgDumpVersion != fakeVersion || onDisk.CloudzillaVersion != "vtest" || onDisk.FormatVersion != backup.FormatVersion {
		t.Errorf("manifest = %+v", onDisk)
	}
	if onDisk.Sections[backup.SectionDatabase] != (backup.Section{Files: 1, Bytes: int64(len(fakeDumpBody))}) {
		t.Errorf("database section = %+v", onDisk.Sections[backup.SectionDatabase])
	}
	if got := onDisk.Sections[backup.SectionHostKey]; got.Files != 1 || got.Bytes != 9 {
		t.Errorf("host key section = %+v", got)
	}
	if m.Sections[backup.SectionGitRepos] != onDisk.Sections[backup.SectionGitRepos] || m.Sections[backup.SectionGitRepos].Files == 0 {
		t.Errorf("returned manifest %+v differs from archived %+v", m.Sections, onDisk.Sections)
	}
	if m.Sections[backup.SectionStorage].Files != 1 {
		t.Errorf("storage section = %+v", m.Sections[backup.SectionStorage])
	}

	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("archive mode = %v, %v; want 0600", info, err)
	}
	left, _ := os.ReadDir(filepath.Dir(out))
	if len(left) != 1 {
		t.Errorf("temp files left beside the archive: %v", left)
	}
}

func TestCreate_OmitsSectionsWhoseSourceIsAbsent(t *testing.T) {
	f := newFixture(t)
	f.opts.ReposRoot = filepath.Join(t.TempDir(), "missing")
	f.opts.StorageRoot = filepath.Join(t.TempDir(), "missing")
	f.opts.HostKeyPath = filepath.Join(t.TempDir(), "missing")
	f.opts.ObjectStorage = &backup.ObjectStorage{Backend: "s3", Bucket: "b"}
	_, m := f.create(t)
	if len(m.Sections) != 1 || m.Sections[backup.SectionDatabase].Files != 1 {
		t.Errorf("sections = %+v, want database only", m.Sections)
	}
	if m.ObjectStorage == nil || m.ObjectStorage.Bucket != "b" {
		t.Errorf("object storage = %+v", m.ObjectStorage)
	}
}

func TestCreate_ToStdout(t *testing.T) {
	f := newFixture(t)
	var buf bytes.Buffer
	if _, err := backup.Create(context.Background(), "-", &buf, f.opts); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&buf)
	h, err := tr.Next()
	if err != nil || h.Name != "cloudzilla-backup.json" {
		t.Fatalf("first entry = %v, %v", h, err)
	}
}

func TestCreate_Refusals(t *testing.T) {
	ctx := context.Background()
	t.Run("output inside the repos root", func(t *testing.T) {
		f := newFixture(t)
		_, err := backup.Create(ctx, filepath.Join(f.reposRoot, "alice", "b.tar"), nil, f.opts)
		wantErr(t, err, "would back itself up")
		if _, err := os.Stat(filepath.Join(f.reposRoot, "alice", "b.tar")); err == nil {
			t.Error("archive written into the repos root")
		}
	})
	t.Run("pg_dump fails", func(t *testing.T) {
		f := newFixture(t)
		f.opts.PgDump = writeTool(t, "pg_dump", `[ "$1" = "--version" ] && { echo "pg_dump (PostgreSQL) 99.0"; exit 0; }
echo "connection refused" >&2; exit 1`)
		dir := t.TempDir()
		_, err := backup.Create(ctx, filepath.Join(dir, "b.tar"), nil, f.opts)
		wantErr(t, err, "connection refused")
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Errorf("left %v behind", left)
		}
	})
	t.Run("unparsable pg_dump version", func(t *testing.T) {
		f := newFixture(t)
		f.opts.PgDump = writeTool(t, "pg_dump", `echo "not a version"`)
		_, err := backup.Create(ctx, filepath.Join(t.TempDir(), "b.tar"), nil, f.opts)
		wantErr(t, err, "unrecognised version")
	})
	t.Run("database without migrations", func(t *testing.T) {
		f := newFixture(t)
		f.opts.DB, _ = newDatabase(t, "bare")
		dir := t.TempDir()
		_, err := backup.Create(ctx, filepath.Join(dir, "b.tar"), nil, f.opts)
		wantErr(t, err, "no applied migrations")
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Errorf("left %v behind", left)
		}
	})
	t.Run("bad DSN", func(t *testing.T) {
		f := newFixture(t)
		f.opts.DSN = "postgres://%zz"
		_, err := backup.Create(ctx, filepath.Join(t.TempDir(), "b.tar"), nil, f.opts)
		wantErr(t, err, "parse database DSN")
	})
	t.Run("unreadable host key", func(t *testing.T) {
		f := newFixture(t)
		f.opts.HostKeyPath = t.TempDir() // a directory
		dir := t.TempDir()
		_, err := backup.Create(ctx, filepath.Join(dir, "b.tar"), nil, f.opts)
		wantErr(t, err, "read ssh host key")
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Errorf("left %v behind", left)
		}
	})
	t.Run("output directory missing", func(t *testing.T) {
		f := newFixture(t)
		_, err := backup.Create(ctx, filepath.Join(t.TempDir(), "no", "such", "b.tar"), nil, f.opts)
		wantErr(t, err, "create temp archive")
	})
	t.Run("canceled context", func(t *testing.T) {
		f := newFixture(t)
		c, cancel := context.WithCancel(ctx)
		cancel()
		dir := t.TempDir()
		if _, err := backup.Create(c, filepath.Join(dir, "b.tar"), nil, f.opts); err == nil {
			t.Fatal("create succeeded with a canceled context")
		}
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Errorf("left %v behind", left)
		}
	})
}

type ent struct {
	name string
	typ  byte
	body string
}

// restoreEnv is a restore target plus the fake pg_restore it runs.
type restoreEnv struct {
	*target
	received string
}

func newRestoreEnv(t *testing.T) *restoreEnv {
	t.Helper()
	tg := newTarget(t)
	tool, received := fakeRestore(t)
	tg.opts.PgRestore = tool
	return &restoreEnv{target: tg, received: received}
}

func (e *restoreEnv) restore(t *testing.T, archive string) (*backup.RestoreReport, error) {
	t.Helper()
	return backup.Restore(context.Background(), archive, nil, e.opts)
}

func (e *restoreEnv) tableCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname = 'public'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *restoreEnv) assertUntouched(t *testing.T) {
	t.Helper()
	if n := e.tableCount(t); n != 0 {
		t.Errorf("%d tables exist after a refused restore", n)
	}
	for _, p := range []string{e.reposRoot, e.storageRoot, e.hostKey, e.received} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s exists after a refused restore", p)
		}
	}
}

func newestMigration(t *testing.T) string {
	t.Helper()
	m, err := db.NewestApplied(context.Background(), testutil.OpenFreshTestDB(t))
	if err != nil || m == "" {
		t.Fatalf("newest migration = %q, %v", m, err)
	}
	return m
}

// writeArchive builds a tar from a manifest and entries. With a nil manifest
// it is generated to match the entries, so the entry under test is the only
// thing wrong with the archive.
func writeArchive(t *testing.T, m *backup.Manifest, ents []ent) string {
	t.Helper()
	if m == nil {
		m = &backup.Manifest{FormatVersion: backup.FormatVersion, Migration: newestMigration(t), PgDumpVersion: fakeVersion, Sections: map[string]backup.Section{}}
		for _, e := range ents {
			if e.typ != tar.TypeReg {
				continue
			}
			var sec string
			switch {
			case e.name == "database.pgdump":
				sec = backup.SectionDatabase
			case e.name == "ssh_host_key":
				sec = backup.SectionHostKey
			case strings.HasPrefix(e.name, "git-repos/"):
				sec = backup.SectionGitRepos
			case strings.HasPrefix(e.name, "storage/"):
				sec = backup.SectionStorage
			default:
				continue
			}
			s := m.Sections[sec]
			s.Files++
			s.Bytes += int64(len(e.body))
			m.Sections[sec] = s
		}
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return writeRaw(t, append([]ent{{"cloudzilla-backup.json", tar.TypeReg, string(body)}}, ents...))
}

func writeRaw(t *testing.T, ents []ent) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crafted.tar")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, e := range ents {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.body))}
		switch e.typ {
		case tar.TypeSymlink, tar.TypeLink:
			h.Linkname, h.Size = "/etc/passwd", 0
		case tar.TypeDir:
			h.Mode, h.Size = 0o755, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

var dumpEnt = ent{"database.pgdump", tar.TypeReg, "dump"}

func TestRestore_RoundTripWithFakeTools(t *testing.T) {
	f := newFixture(t)
	archive, _ := f.create(t)

	e := newRestoreEnv(t)
	rep, err := e.restore(t, archive)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got, _ := os.ReadFile(e.received); string(got) != fakeDumpBody {
		t.Errorf("pg_restore was fed %q", got)
	}
	args, _ := os.ReadFile(e.received + ".args")
	for _, want := range []string{"--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction", "--dbname=" + dbName(t, e.db)} {
		if !strings.Contains(string(args), want) {
			t.Errorf("pg_restore args %q lack %s", args, want)
		}
	}
	if pending, _ := db.Pending(context.Background(), e.db); len(pending) != 0 {
		t.Errorf("migrations pending after restore: %v", pending)
	}
	if rep.Manifest == nil || rep.Manifest.CloudzillaVersion != "vtest" {
		t.Errorf("report manifest = %+v", rep.Manifest)
	}

	if _, err := gogit.PlainOpen(filepath.Join(e.reposRoot, "alice", "proj.git")); err != nil {
		t.Errorf("restored repository does not open: %v", err)
	}
	for _, p := range []string{"alice/proj.git/refs/heads/main.lock", "alice/proj.git/objects/pack/tmp_pack_abc", ".import-tmp", "alice/link"} {
		if _, err := os.Lstat(filepath.Join(e.reposRoot, p)); err == nil {
			t.Errorf("%s was restored", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(e.reposRoot, "alice", "proj.git", "refs", "heads", "main")); !strings.HasPrefix(string(b), "0123456789") {
		t.Errorf("ref = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(e.storageRoot, "avatars", "user", "1", "a.png")); string(b) != "png" {
		t.Errorf("avatar = %q", b)
	}
	if b, _ := os.ReadFile(e.hostKey); string(b) != "HOST-KEY\n" {
		t.Errorf("host key = %q", b)
	}
	if info, _ := os.Stat(e.hostKey); info.Mode().Perm() != 0o600 {
		t.Errorf("host key mode = %v", info.Mode().Perm())
	}
	// ListRepos sees no rows in a database that was never really restored.
	if !slices.Equal(rep.OrphanDirs, []string{"alice/proj.git"}) {
		t.Errorf("orphans = %v", rep.OrphanDirs)
	}
}

func dbName(t *testing.T, d *sql.DB) string {
	t.Helper()
	var n string
	if err := d.QueryRow(`SELECT current_database()`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRestore_FromStdinAndMissingFile(t *testing.T) {
	archive := writeArchive(t, nil, []ent{dumpEnt})
	e := newRestoreEnv(t)
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var log bytes.Buffer
	e.opts.Log = &log
	if _, err := backup.Restore(context.Background(), "-", f, e.opts); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(e.received); string(got) != "dump" {
		t.Errorf("dump = %q", got)
	}
	if !strings.Contains(log.String(), "restoring database") || !strings.Contains(log.String(), "applying newer migrations") {
		t.Errorf("log = %q", log.String())
	}

	e2 := newRestoreEnv(t)
	_, err = e2.restore(t, filepath.Join(t.TempDir(), "absent.tar"))
	if err == nil || !os.IsNotExist(unwrapAll(err)) {
		t.Errorf("missing archive err = %v", err)
	}
	e2.assertUntouched(t)
}

func unwrapAll(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
}

func TestRestore_RejectsUnsafeEntriesBeforeWriting(t *testing.T) {
	cases := []struct {
		name string
		ent  ent
		want string
	}{
		{"parent traversal", ent{"git-repos/../../escape", tar.TypeReg, "x"}, "unsafe path"},
		{"nested traversal", ent{"git-repos/a/../../../escape", tar.TypeReg, "x"}, "unsafe path"},
		{"storage traversal", ent{"storage/../../escape", tar.TypeReg, "x"}, "unsafe path"},
		{"bare dotdot", ent{"..", tar.TypeDir, ""}, "unsafe path"},
		{"absolute", ent{"/etc/cron.d/x", tar.TypeReg, "x"}, "unsafe path"},
		{"absolute below section", ent{"git-repos//etc/x", tar.TypeReg, "x"}, "unsafe path"},
		{"dot segment", ent{"git-repos/./x", tar.TypeReg, "x"}, "unsafe path"},
		{"double slash", ent{"git-repos/a//x", tar.TypeReg, "x"}, "unsafe path"},
		{"empty name", ent{"", tar.TypeReg, "x"}, "unsafe path"},
		{"symlink", ent{"git-repos/link", tar.TypeSymlink, ""}, "unsupported type"},
		{"hard link", ent{"git-repos/hard", tar.TypeLink, ""}, "unsupported type"},
		{"fifo", ent{"git-repos/pipe", tar.TypeFifo, ""}, "unsupported type"},
		{"unknown top-level entry", ent{"etc/passwd", tar.TypeReg, "x"}, "unexpected entry"},
		{"dump as a directory", ent{"database.pgdump", tar.TypeDir, ""}, "unsupported type"},
		{"repos root as a file", ent{"git-repos", tar.TypeReg, "x"}, "unsupported type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			e := newRestoreEnv(t)
			e.opts.ReposRoot = filepath.Join(parent, "repos")
			e.opts.StorageRoot = filepath.Join(parent, "storage")
			e.opts.HostKeyPath = filepath.Join(parent, "host_key")
			e.reposRoot, e.storageRoot, e.hostKey = e.opts.ReposRoot, e.opts.StorageRoot, e.opts.HostKeyPath

			_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, c.ent}))
			wantErr(t, err, c.want)
			if left, _ := os.ReadDir(parent); len(left) != 0 {
				t.Errorf("refused restore created %v", left)
			}
			e.assertUntouched(t)
		})
	}
}

func TestRestore_RejectsDamagedArchives(t *testing.T) {
	valid := func(extra ...ent) []ent { return append([]ent{dumpEnt}, extra...) }
	manifestOf := func(sections map[string]backup.Section) *backup.Manifest {
		return &backup.Manifest{FormatVersion: backup.FormatVersion, Migration: newestMigration(t), PgDumpVersion: fakeVersion, Sections: sections}
	}
	cases := []struct {
		name    string
		archive func(t *testing.T) string
		want    string
	}{
		{"not a tar", func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "x")
			_ = os.WriteFile(p, bytes.Repeat([]byte("garbage!"), 200), 0o600)
			return p
		}, "read backup"},
		{"empty file", func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "x")
			_ = os.WriteFile(p, nil, 0o600)
			return p
		}, "archive is empty"},
		{"no manifest first", func(t *testing.T) string { return writeRaw(t, []ent{dumpEnt}) }, "must be the first entry"},
		{"manifest twice", func(t *testing.T) string {
			return writeArchive(t, nil, valid(ent{"cloudzilla-backup.json", tar.TypeReg, "{}"}))
		}, "must be the first entry"},
		{"manifest not JSON", func(t *testing.T) string {
			return writeRaw(t, []ent{{"cloudzilla-backup.json", tar.TypeReg, "not json"}, dumpEnt})
		}, "read cloudzilla-backup.json"},
		{"no database", func(t *testing.T) string {
			return writeArchive(t, nil, []ent{{"ssh_host_key", tar.TypeReg, "k"}})
		}, "no database.pgdump"},
		{"database twice", func(t *testing.T) string {
			return writeArchive(t, nil, valid(dumpEnt))
		}, "appears twice"},
		{"host key twice", func(t *testing.T) string {
			return writeArchive(t, nil, valid(ent{"ssh_host_key", tar.TypeReg, "k"}, ent{"ssh_host_key", tar.TypeReg, "k"}))
		}, "appears twice"},
		{"manifest undercounts files", func(t *testing.T) string {
			return writeArchive(t, manifestOf(map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 4}}),
				valid(ent{"git-repos/a", tar.TypeReg, "x"}))
		}, "damaged"},
		{"manifest overstates bytes", func(t *testing.T) string {
			return writeArchive(t, manifestOf(map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 5000}}), valid())
		}, "damaged"},
		{"manifest lists a section the archive lacks", func(t *testing.T) string {
			return writeArchive(t, manifestOf(map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 4}, backup.SectionStorage: {Files: 2, Bytes: 9}}), valid())
		}, "damaged"},
		{"truncated mid-entry", func(t *testing.T) string {
			full, _ := os.ReadFile(writeArchive(t, nil, valid(ent{"git-repos/a", tar.TypeReg, strings.Repeat("x", 3000)})))
			p := filepath.Join(t.TempDir(), "x")
			_ = os.WriteFile(p, full[:len(full)-3000], 0o600)
			return p
		}, "read backup"},
		{"unknown format version", func(t *testing.T) string {
			return writeArchive(t, &backup.Manifest{FormatVersion: 99}, valid())
		}, "format version 99"},
		{"migration newer than the binary", func(t *testing.T) string {
			m := manifestOf(map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 4}})
			m.Migration = "999_from_the_future"
			return writeArchive(t, m, valid())
		}, "999_from_the_future"},
		{"unparsable pg_dump version", func(t *testing.T) string {
			m := manifestOf(map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 4}})
			m.PgDumpVersion = "garbage"
			return writeArchive(t, m, valid())
		}, "backup records pg_dump version"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRestoreEnv(t)
			_, err := e.restore(t, c.archive(t))
			wantErr(t, err, c.want)
			e.assertUntouched(t)
		})
	}
}

func TestRestore_RefusesNonEmptyTargets(t *testing.T) {
	archive := writeArchive(t, nil, []ent{dumpEnt, {"git-repos/a.txt", tar.TypeReg, "x"}, {"storage/s.txt", tar.TypeReg, "x"}})

	t.Run("repos root", func(t *testing.T) {
		e := newRestoreEnv(t)
		_ = os.MkdirAll(e.reposRoot, 0o755)
		_ = os.WriteFile(filepath.Join(e.reposRoot, "stray"), []byte("keep"), 0o644)
		_, err := e.restore(t, archive)
		wantErr(t, err, "not empty")
		if b, _ := os.ReadFile(filepath.Join(e.reposRoot, "stray")); string(b) != "keep" {
			t.Error("existing file touched")
		}
		if e.tableCount(t) != 0 {
			t.Error("database written")
		}
	})
	t.Run("storage root", func(t *testing.T) {
		e := newRestoreEnv(t)
		_ = os.MkdirAll(e.storageRoot, 0o755)
		_ = os.WriteFile(filepath.Join(e.storageRoot, "stray"), []byte("keep"), 0o644)
		_, err := e.restore(t, archive)
		wantErr(t, err, "not empty")
		if _, err := os.Stat(e.reposRoot); err == nil {
			t.Error("repos root created")
		}
	})
	t.Run("migrated database", func(t *testing.T) {
		e := newRestoreEnv(t)
		e.opts.DB, _ = migratedDatabase(t, "used")
		_, err := e.restore(t, archive)
		wantErr(t, err, "schema_migrations")
		if _, err := os.Stat(e.reposRoot); err == nil {
			t.Error("repos root created")
		}
	})
	t.Run("database with a stray table", func(t *testing.T) {
		e := newRestoreEnv(t)
		testutil.Exec(t, e.db, `CREATE TABLE stray (id int)`)
		_, err := e.restore(t, archive)
		wantErr(t, err, "already has 1 tables")
		if _, err := os.Stat(e.reposRoot); err == nil {
			t.Error("repos root created")
		}
	})
}

func TestRestore_HostKeyPlanning(t *testing.T) {
	archive := writeArchive(t, nil, []ent{dumpEnt, {"ssh_host_key", tar.TypeReg, "NEW-KEY"}})

	t.Run("path not configured", func(t *testing.T) {
		e := newRestoreEnv(t)
		e.opts.HostKeyPath = ""
		_, err := e.restore(t, archive)
		wantErr(t, err, "git.ssh_host_key is not set")
		if e.tableCount(t) != 0 {
			t.Error("database written")
		}
	})
	t.Run("existing path unreadable", func(t *testing.T) {
		e := newRestoreEnv(t)
		e.opts.HostKeyPath = t.TempDir() // a directory
		_, err := e.restore(t, archive)
		wantErr(t, err, "read existing host key")
	})
	t.Run("identical key is left alone", func(t *testing.T) {
		e := newRestoreEnv(t)
		_ = os.WriteFile(e.hostKey, []byte("NEW-KEY"), 0o644)
		if _, err := e.restore(t, archive); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Stat(e.hostKey); info.Mode().Perm() != 0o644 {
			t.Errorf("identical key was rewritten (mode %v)", info.Mode().Perm())
		}
	})
	t.Run("different key refused then replaced", func(t *testing.T) {
		e := newRestoreEnv(t)
		_ = os.WriteFile(e.hostKey, []byte("OLD-KEY"), 0o600)
		_, err := e.restore(t, archive)
		wantErr(t, err, "--replace-host-key")
		if b, _ := os.ReadFile(e.hostKey); string(b) != "OLD-KEY" {
			t.Errorf("key overwritten without the flag: %q", b)
		}
		if e.tableCount(t) != 0 {
			t.Error("database written")
		}
		e.opts.ReplaceHostKey = true
		if _, err := e.restore(t, archive); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(e.hostKey); string(b) != "NEW-KEY" {
			t.Errorf("key = %q", b)
		}
		if left, _ := os.ReadDir(filepath.Dir(e.hostKey)); len(left) != 1 {
			t.Errorf("temp files left beside the host key: %v", left)
		}
	})
	t.Run("parent directory is created", func(t *testing.T) {
		e := newRestoreEnv(t)
		e.opts.HostKeyPath = filepath.Join(t.TempDir(), "deep", "er", "key")
		if _, err := e.restore(t, archive); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(e.opts.HostKeyPath); string(b) != "NEW-KEY" {
			t.Errorf("key = %q", b)
		}
	})
	t.Run("parent is a file", func(t *testing.T) {
		e := newRestoreEnv(t)
		blocker := filepath.Join(t.TempDir(), "blocker")
		_ = os.WriteFile(blocker, nil, 0o600)
		e.opts.HostKeyPath = filepath.Join(blocker, "key")
		_, err := e.restore(t, archive)
		wantErr(t, err, "read existing host key")
		e.assertUntouched(t)
	})
}

func TestRestore_Warnings(t *testing.T) {
	t.Run("s3 backend", func(t *testing.T) {
		m := &backup.Manifest{FormatVersion: backup.FormatVersion, Migration: newestMigration(t), PgDumpVersion: fakeVersion,
			Sections: map[string]backup.Section{backup.SectionDatabase: {Files: 1, Bytes: 4}}, ObjectStorage: &backup.ObjectStorage{Backend: "s3", Bucket: "bkt"}}
		e := newRestoreEnv(t)
		rep, err := e.restore(t, writeArchive(t, m, []ent{dumpEnt}))
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], `bucket "bkt"`) {
			t.Errorf("warnings = %v", rep.Warnings)
		}
	})
	t.Run("storage files with no local storage configured", func(t *testing.T) {
		e := newRestoreEnv(t)
		e.opts.StorageRoot = ""
		rep, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, {"storage/", tar.TypeDir, ""}, {"storage/a.png", tar.TypeReg, "x"}}))
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "were not restored") {
			t.Errorf("warnings = %v", rep.Warnings)
		}
		if _, err := os.Stat(e.storageRoot); err == nil {
			t.Error("storage root created")
		}
	})
}

func TestRestore_PgRestoreProblemsWriteNothingToDisk(t *testing.T) {
	archive := writeArchive(t, nil, []ent{dumpEnt, {"git-repos/a.txt", tar.TypeReg, "x"}, {"ssh_host_key", tar.TypeReg, "k"}})
	cases := map[string]func(t *testing.T) string{
		"fails": func(t *testing.T) string {
			return writeTool(t, "pg_restore", `[ "$1" = "--version" ] && { echo "pg_restore (PostgreSQL) 99.0"; exit 0; }
cat >/dev/null; echo "role missing" >&2; exit 1`)
		},
		"missing": func(t *testing.T) string { return filepath.Join(t.TempDir(), "nope") },
		"too old": func(t *testing.T) string {
			return writeTool(t, "pg_restore", `echo "pg_restore (PostgreSQL) 9.6.24"`)
		},
		"broken version output": func(t *testing.T) string { return writeTool(t, "pg_restore", `echo hello`) },
		"version exits non-zero": func(t *testing.T) string {
			return writeTool(t, "pg_restore", `exit 3`)
		},
	}
	wants := map[string]string{"fails": "role missing", "missing": "postgresql99-client", "too old": "older than the backup", "broken version output": "unrecognised version", "version exits non-zero": "--version"}
	for name, tool := range cases {
		t.Run(name, func(t *testing.T) {
			e := newRestoreEnv(t)
			e.opts.PgRestore = tool(t)
			_, err := e.restore(t, archive)
			wantErr(t, err, wants[name])
			if _, err := os.Stat(e.reposRoot); err == nil {
				t.Error("repos root created before the database was restored")
			}
			if _, err := os.Stat(e.hostKey); err == nil {
				t.Error("host key written")
			}
		})
	}
}

func TestRestore_BadTargetDSN(t *testing.T) {
	e := newRestoreEnv(t)
	e.opts.DSN = "postgres://%zz"
	_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt}))
	wantErr(t, err, "parse database DSN")
	e.assertUntouched(t)
}

func TestRestore_ExtractionFailureNamesTheCleanup(t *testing.T) {
	t.Run("duplicate repository file", func(t *testing.T) {
		e := newRestoreEnv(t)
		_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, {"git-repos/a.txt", tar.TypeReg, "first"}, {"git-repos/a.txt", tar.TypeReg, "second"}}))
		wantErr(t, err, "empty git.repos_root and drop the database")
		if b, _ := os.ReadFile(filepath.Join(e.reposRoot, "a.txt")); string(b) != "first" {
			t.Errorf("the duplicate overwrote the first file: %q", b)
		}
		if _, err := os.Stat(e.hostKey); err == nil {
			t.Error("host key written after a failed extraction")
		}
	})
	t.Run("file where a directory is needed", func(t *testing.T) {
		e := newRestoreEnv(t)
		_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, {"git-repos/a", tar.TypeReg, "f"}, {"git-repos/a/b", tar.TypeReg, "g"}}))
		wantErr(t, err, "git-repos/a/b")
	})
	t.Run("duplicate storage file", func(t *testing.T) {
		e := newRestoreEnv(t)
		_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, {"storage/s", tar.TypeReg, "1"}, {"storage/s", tar.TypeReg, "2"}}))
		wantErr(t, err, "empty storage.local.root")
	})
	t.Run("directory entry onto an existing file", func(t *testing.T) {
		e := newRestoreEnv(t)
		_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, {"git-repos/a", tar.TypeReg, "f"}, {"git-repos/a/", tar.TypeDir, ""}}))
		wantErr(t, err, "git-repos/a/")
	})
	t.Run("storage directory entry onto an existing file", func(t *testing.T) {
		e := newRestoreEnv(t)
		_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt, {"storage/a", tar.TypeReg, "f"}, {"storage/a/", tar.TypeDir, ""}}))
		wantErr(t, err, "storage/a/")
	})
}

func TestRestore_ReconcilesRowsAgainstDirectories(t *testing.T) {
	e := newRestoreEnv(t)
	e.opts.ListRepos = func(context.Context) ([]backup.RepoRef, error) {
		return []backup.RepoRef{{Owner: "o", Name: "present"}, {Owner: "o", Name: "gone"}, {Owner: "o", Name: "wikionly"}}, nil
	}
	rep, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt,
		{"git-repos/", tar.TypeDir, ""},
		{"git-repos/o/present.git/HEAD", tar.TypeReg, "ref"},
		{"git-repos/o/present.wiki.git/HEAD", tar.TypeReg, "ref"},
		{"git-repos/o/stray.git/HEAD", tar.TypeReg, "ref"},
		{"git-repos/o/stray.wiki.git/HEAD", tar.TypeReg, "ref"},
		{"git-repos/o/present.deleted.1700000000/HEAD", tar.TypeReg, "ref"},
		{"git-repos/o/readme.git", tar.TypeReg, "a file, not a repository"},
		{"git-repos/loose-file", tar.TypeReg, "x"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"o/gone", "o/wikionly"}; !slices.Equal(rep.MissingDirs, want) {
		t.Errorf("missing = %v, want %v", rep.MissingDirs, want)
	}
	if want := []string{"o/stray.git", "o/stray.wiki.git"}; !slices.Equal(rep.OrphanDirs, want) {
		t.Errorf("orphans = %v, want %v", rep.OrphanDirs, want)
	}
}

func TestRestore_ListReposFailure(t *testing.T) {
	e := newRestoreEnv(t)
	e.opts.ListRepos = func(context.Context) ([]backup.RepoRef, error) { return nil, io.ErrUnexpectedEOF }
	_, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt}))
	wantErr(t, err, "listing repositories failed")
}

func TestRestore_RowsWithNoRepositoriesDirectory(t *testing.T) {
	e := newRestoreEnv(t)
	e.opts.ListRepos = func(context.Context) ([]backup.RepoRef, error) {
		return []backup.RepoRef{{Owner: "o", Name: "r"}}, nil
	}
	rep, err := e.restore(t, writeArchive(t, nil, []ent{dumpEnt}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.MissingDirs, []string{"o/r"}) || len(rep.OrphanDirs) != 0 {
		t.Errorf("report = %+v", rep)
	}
}
