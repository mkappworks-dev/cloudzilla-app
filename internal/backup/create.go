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
	"strconv"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
)

type CreateOptions struct {
	DB          *sql.DB
	DSN         string
	PgDump      string // a name on PATH or a path; empty means pg_dump
	ReposRoot   string
	HostKeyPath string
	// Set only when storage.backend is local; an S3 bucket is not copied.
	StorageRoot string
	Version     string
	// Recorded in the manifest when the backend holds data the archive does not.
	ObjectStorage *ObjectStorage

	afterRefs func(gitDir string)
}

// Create writes the archive to output ("-" is stdout). It builds in a temp
// file beside output and renames at the end, so a failure leaves no partial
// archive. The database is dumped before any repository is read.
func Create(ctx context.Context, output string, stdout io.Writer, opts CreateOptions) (*Manifest, error) {
	toStdout := output == "-"
	if opts.PgDump == "" {
		opts.PgDump = "pg_dump"
	}
	if !toStdout {
		absOut, _ := filepath.Abs(output)
		absRoot, _ := filepath.Abs(opts.ReposRoot)
		if rel, err := filepath.Rel(absRoot, absOut); err == nil && filepath.IsLocal(rel) {
			return nil, fmt.Errorf("--output %s is inside git.repos_root and would back itself up", output)
		}
	}

	var versionNum string
	if err := opts.DB.QueryRowContext(ctx, "SHOW server_version_num").Scan(&versionNum); err != nil {
		return nil, fmt.Errorf("read server version: %w", err)
	}
	num, err := strconv.Atoi(versionNum)
	if err != nil {
		return nil, fmt.Errorf("parse server_version_num %q: %w", versionNum, err)
	}
	pgVersion, err := CheckClient(ctx, opts.PgDump, ServerMajor(num), "the server")
	if err != nil {
		return nil, err
	}
	migration, err := db.NewestApplied(ctx, opts.DB)
	if err != nil {
		return nil, err
	}
	if migration == "" {
		return nil, errors.New("the database has no applied migrations: run `cz-admin migrate` first, or there is nothing to back up")
	}
	connEnv, _, err := ConnEnv(opts.DSN)
	if err != nil {
		return nil, err
	}

	tmpDir := os.TempDir()
	if !toStdout {
		tmpDir = filepath.Dir(output)
	}
	tmp, err := os.CreateTemp(tmpDir, ".cloudzilla-backup-*")
	if err != nil {
		return nil, fmt.Errorf("create temp archive: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	m := &Manifest{
		FormatVersion: FormatVersion, CloudzillaVersion: opts.Version, CreatedAt: time.Now().UTC(),
		Migration: migration, PgDumpVersion: pgVersion, Sections: map[string]Section{}, ObjectStorage: opts.ObjectStorage,
	}
	if err := build(ctx, tmp, m, opts, connEnv, tmpDir); err != nil {
		return nil, err
	}

	if toStdout {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		_, err := io.Copy(stdout, tmp)
		return m, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	return m, os.Rename(tmp.Name(), output)
}

func build(ctx context.Context, f *os.File, m *Manifest, opts CreateOptions, connEnv []string, tmpDir string) error {
	tw := tar.NewWriter(f)
	// USTAR keeps the manifest header to one block, so its data sits at tarBlock.
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Typeflag: tar.TypeReg, Size: manifestSlot, Mode: 0o600, ModTime: m.CreatedAt, Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	if _, err := tw.Write(make([]byte, manifestSlot)); err != nil {
		return err
	}

	dump, err := dumpDatabase(ctx, opts.PgDump, connEnv, tmpDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = dump.Close()
		_ = os.Remove(dump.Name())
	}()
	size, err := dump.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := dump.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: dumpName, Typeflag: tar.TypeReg, Size: size, Mode: 0o600, ModTime: m.CreatedAt}); err != nil {
		return err
	}
	if _, err := io.Copy(tw, dump); err != nil {
		return err
	}
	m.Sections[SectionDatabase] = Section{Files: 1, Bytes: size}

	if _, err := os.Stat(opts.ReposRoot); err == nil {
		c := &repoCopier{tw: tw, root: opts.ReposRoot, prefix: reposDir, afterRefs: opts.afterRefs}
		if err := c.run(ctx); err != nil {
			return fmt.Errorf("copy repositories: %w", err)
		}
		m.Sections[SectionGitRepos] = c.stats
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if opts.StorageRoot != "" {
		if _, err := os.Stat(opts.StorageRoot); err == nil {
			c := &repoCopier{tw: tw, root: opts.StorageRoot, prefix: storageDir, plain: true}
			if err := c.run(ctx); err != nil {
				return fmt.Errorf("copy storage: %w", err)
			}
			m.Sections[SectionStorage] = c.stats
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	if opts.HostKeyPath != "" {
		key, err := os.ReadFile(opts.HostKeyPath)
		switch {
		case err == nil:
			if err := tw.WriteHeader(&tar.Header{Name: hostKeyName, Typeflag: tar.TypeReg, Size: int64(len(key)), Mode: 0o600, ModTime: m.CreatedAt}); err != nil {
				return err
			}
			if _, err := tw.Write(key); err != nil {
				return err
			}
			m.Sections[SectionHostKey] = Section{Files: 1, Bytes: int64(len(key))}
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("read ssh host key: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	slot, err := m.slot()
	if err != nil {
		return err
	}
	_, err = f.WriteAt(slot, tarBlock)
	return err
}

// dumpDatabase spools pg_dump's output to a temp file: the tar header needs
// the size before the data.
func dumpDatabase(ctx context.Context, tool string, connEnv []string, dir string) (*os.File, error) {
	spool, err := os.CreateTemp(dir, ".cloudzilla-dump-*")
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, tool, "--format=custom", "--no-owner", "--no-privileges")
	cmd.Env = append(os.Environ(), connEnv...)
	cmd.Stdout = spool
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
		return nil, fmt.Errorf("pg_dump failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return spool, nil
}
