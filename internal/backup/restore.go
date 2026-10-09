package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
)

const maxHostKeyBytes = 1 << 20

// RepoRef is a repository row, for the reconciliation report.
type RepoRef struct{ Owner, Name string }

type RestoreOptions struct {
	DB          *sql.DB
	DSN         string
	PgRestore   string // a name on PATH or a path; empty means pg_restore
	ReposRoot   string
	HostKeyPath string
	// Empty unless storage.backend is local.
	StorageRoot    string
	ReplaceHostKey bool
	ListRepos      func(ctx context.Context) ([]RepoRef, error)
	Log            io.Writer
}

type RestoreReport struct {
	Manifest *Manifest
	// "owner/name" of rows with no directory, and of directories with no row.
	MissingDirs []string
	OrphanDirs  []string
	Warnings    []string
}

type scanResult struct {
	manifest *Manifest
	hostKey  []byte
	hasStore bool
}

// Restore rebuilds an empty instance from input ("-" is stdin). It reads the
// whole archive and checks every precondition before it writes anything.
func Restore(ctx context.Context, input string, stdin io.Reader, opts RestoreOptions) (*RestoreReport, error) {
	if opts.PgRestore == "" {
		opts.PgRestore = "pg_restore"
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}

	src, cleanup, err := openInput(input, stdin)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	scan, err := scanArchive(src)
	if err != nil {
		return nil, err
	}
	m := scan.manifest
	report := &RestoreReport{Manifest: m}

	if err := rootEmpty(opts.ReposRoot); err != nil {
		return nil, err
	}
	restoreStorage := scan.hasStore && opts.StorageRoot != ""
	if restoreStorage {
		if err := rootEmpty(opts.StorageRoot); err != nil {
			return nil, err
		}
	}
	writeKey, err := planHostKey(opts, scan.hostKey)
	if err != nil {
		return nil, err
	}
	known, err := db.MigrationKnown(m.Migration)
	if err != nil {
		return nil, err
	}
	if !known {
		return nil, fmt.Errorf("the backup's newest migration %q is not in this binary: restore with the release that took the backup, or a newer one", m.Migration)
	}
	if err := requireEmptyDatabase(ctx, opts.DB); err != nil {
		return nil, err
	}
	dumpMajor, _, err := ParsePgVersion("(PostgreSQL) " + m.PgDumpVersion)
	if err != nil {
		return nil, fmt.Errorf("backup records pg_dump version %q: %w", m.PgDumpVersion, err)
	}
	if _, err := CheckClient(ctx, opts.PgRestore, dumpMajor, "the backup"); err != nil {
		return nil, err
	}
	connEnv, dbname, err := ConnEnv(opts.DSN)
	if err != nil {
		return nil, err
	}
	if o := m.ObjectStorage; o != nil && o.Backend != "" && o.Backend != "local" {
		report.Warnings = append(report.Warnings, fmt.Sprintf("the backup used %s object storage (bucket %q), which it does not contain: restore the bucket separately", o.Backend, o.Bucket))
	}
	if scan.hasStore && !restoreStorage {
		report.Warnings = append(report.Warnings, "the backup holds local storage files (avatars) but storage.backend is not local here: they were not restored")
	}

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	tr := tar.NewReader(src)
	x := extractor{root: opts.ReposRoot}
	sx := extractor{root: opts.StorageRoot}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		section, rel, err := checkEntry(h.Name, h.Typeflag)
		if err != nil {
			return nil, err
		}
		switch section {
		case SectionDatabase:
			_, _ = fmt.Fprintln(opts.Log, "restoring database")
			if err := runPgRestore(ctx, opts.PgRestore, connEnv, dbname, tr); err != nil {
				return nil, err
			}
		case SectionGitRepos:
			if h.Typeflag == tar.TypeDir {
				err = x.dir(rel, h.FileInfo().Mode())
			} else {
				_, err = x.file(rel, tr, h.FileInfo().Mode(), h.ModTime)
			}
			if err != nil {
				return nil, fmt.Errorf("the database is restored but extracting %s failed: %w: empty git.repos_root and drop the database before retrying", h.Name, err)
			}
		case SectionStorage:
			if !restoreStorage {
				continue
			}
			if h.Typeflag == tar.TypeDir {
				err = sx.dir(rel, h.FileInfo().Mode())
			} else {
				_, err = sx.file(rel, tr, h.FileInfo().Mode(), h.ModTime)
			}
			if err != nil {
				return nil, fmt.Errorf("the database is restored but extracting %s failed: %w: empty storage.local.root and git.repos_root and drop the database before retrying", h.Name, err)
			}
		case SectionHostKey:
			if writeKey {
				if err := writeHostKey(opts.HostKeyPath, scan.hostKey); err != nil {
					return nil, fmt.Errorf("the database and repositories are restored but writing the host key failed: %w", err)
				}
			}
		}
	}

	_, _ = fmt.Fprintln(opts.Log, "applying newer migrations")
	if err := db.Migrate(opts.DB); err != nil {
		return nil, fmt.Errorf("the backup is restored but migrating failed: %w", err)
	}
	if opts.ListRepos != nil {
		rows, err := opts.ListRepos(ctx)
		if err != nil {
			return nil, fmt.Errorf("the backup is restored but listing repositories failed: %w", err)
		}
		report.MissingDirs, report.OrphanDirs, err = reconcile(opts.ReposRoot, rows)
		if err != nil {
			return nil, err
		}
	}
	return report, nil
}

// openInput returns a seekable file: the archive is read twice, once to
// validate and once to restore, and stdin can't be re-read.
func openInput(input string, stdin io.Reader) (*os.File, func(), error) {
	if input != "-" {
		f, err := os.Open(input)
		if err != nil {
			return nil, nil, err
		}
		return f, func() { _ = f.Close() }, nil
	}
	spool, err := os.CreateTemp("", ".cloudzilla-restore-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}
	if _, err := io.Copy(spool, stdin); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("read backup from stdin: %w", err)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, nil, err
	}
	return spool, cleanup, nil
}

// scanArchive validates every entry and checks the manifest's counts against
// what the archive holds, without writing anything.
func scanArchive(r io.Reader) (*scanResult, error) {
	tr := tar.NewReader(r)
	res := &scanResult{}
	counts := map[string]Section{}
	for i := 0; ; i++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read backup: %w", err)
		}
		section, _, err := checkEntry(h.Name, h.Typeflag)
		if err != nil {
			return nil, err
		}
		if (i == 0) != (section == SectionManifest) {
			return nil, fmt.Errorf("%s must be the first entry, exactly once (found %q at position %d)", manifestName, h.Name, i+1)
		}
		switch section {
		case SectionManifest:
			if res.manifest, err = decodeManifest(tr); err != nil {
				return nil, err
			}
			continue
		case SectionHostKey:
			if res.hostKey != nil {
				return nil, fmt.Errorf("%s appears twice", hostKeyName)
			}
			if res.hostKey, err = io.ReadAll(io.LimitReader(tr, maxHostKeyBytes)); err != nil {
				return nil, fmt.Errorf("read %s: %w", hostKeyName, err)
			}
		case SectionDatabase:
			if counts[SectionDatabase].Files > 0 {
				return nil, fmt.Errorf("%s appears twice", dumpName)
			}
		}
		if section == SectionStorage {
			res.hasStore = true
		}
		if h.Typeflag == tar.TypeReg {
			c := counts[section]
			c.Files++
			c.Bytes += h.Size
			counts[section] = c
		}
	}
	if res.manifest == nil {
		return nil, errors.New("not a Cloudzilla backup: the archive is empty")
	}
	if counts[SectionDatabase].Files == 0 {
		return nil, fmt.Errorf("the backup has no %s", dumpName)
	}
	for _, name := range []string{SectionDatabase, SectionGitRepos, SectionStorage, SectionHostKey} {
		if got, want := counts[name], res.manifest.Sections[name]; got != want {
			return nil, fmt.Errorf("the backup is damaged: the manifest lists %d files and %d bytes for %s, the archive holds %d and %d", want.Files, want.Bytes, name, got.Files, got.Bytes)
		}
	}
	return res, nil
}

// planHostKey reports whether the key must be written, refusing before any
// write when a different key is in place.
func planHostKey(opts RestoreOptions, key []byte) (bool, error) {
	if key == nil {
		return false, nil
	}
	if opts.HostKeyPath == "" {
		return false, errors.New("the backup holds an SSH host key but git.ssh_host_key is not set")
	}
	existing, err := os.ReadFile(opts.HostKeyPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("read existing host key: %w", err)
	case bytes.Equal(existing, key):
		return false, nil
	case !opts.ReplaceHostKey:
		return false, fmt.Errorf("a different SSH host key already exists at %s (the server generates one on first boot): remove it, or pass --replace-host-key to overwrite it", opts.HostKeyPath)
	}
	return true, nil
}

func writeHostKey(path string, key []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".host-key-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(key); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// requireEmptyDatabase refuses a database with a schema_migrations table, and
// one with any other table, which would make pg_restore fail midway.
func requireEmptyDatabase(ctx context.Context, d *sql.DB) error {
	var migrated bool
	var tables int
	err := d.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL,
		(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relkind IN ('r','p') AND n.nspname = current_schema())`).Scan(&migrated, &tables)
	if err != nil {
		return fmt.Errorf("inspect target database: %w", err)
	}
	if migrated {
		return errors.New("the database already has a schema_migrations table: restore only runs into a new, empty database")
	}
	if tables > 0 {
		return fmt.Errorf("the database already has %d tables: restore only runs into a new, empty database", tables)
	}
	return nil
}

func runPgRestore(ctx context.Context, tool string, connEnv []string, dbname string, dump io.Reader) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, tool, "--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction", "--dbname="+dbname)
	cmd.Env = append(os.Environ(), connEnv...)
	cmd.Stdin = dump
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore failed (nothing was written to the database): %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// reconcile compares repository rows with the directories on disk. Soft-deleted
// copies (.deleted.<unix>) belong to no live row by design and are ignored.
func reconcile(root string, rows []RepoRef) (missing, orphan []string, err error) {
	have := map[string]bool{}
	for _, r := range rows {
		have[r.Owner+"/"+r.Name] = true
		if _, err := os.Stat(filepath.Join(root, r.Owner, r.Name+".git")); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, nil, err
			}
			missing = append(missing, r.Owner+"/"+r.Name)
		}
	}
	owners, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	for _, o := range owners {
		if !o.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, o.Name()))
		if err != nil {
			return nil, nil, err
		}
		for _, e := range entries {
			base, ok := strings.CutSuffix(e.Name(), ".git")
			if !e.IsDir() || !ok {
				continue
			}
			// A wiki's row is its repository's.
			key := o.Name() + "/" + strings.TrimSuffix(base, ".wiki")
			if !have[key] && !have[o.Name()+"/"+base] {
				orphan = append(orphan, o.Name()+"/"+e.Name())
			}
		}
	}
	slices.Sort(missing)
	slices.Sort(orphan)
	return missing, orphan, nil
}
