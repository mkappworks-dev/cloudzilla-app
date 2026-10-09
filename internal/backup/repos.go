package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// repoCopier writes the tree under root into a tar below prefix. Within each
// repository the refs go in before objects/: git writes an object before it
// moves a ref to it, so every ref in the copy resolves even when a push lands
// mid-copy. A plain copier (the storage root) has no repositories to order.
type repoCopier struct {
	tw     *tar.Writer
	root   string
	prefix string
	plain  bool
	stats  Section
	// Test seam: runs once per repository, between its refs and its objects.
	afterRefs func(gitDir string)
}

func (c *repoCopier) run(ctx context.Context) error {
	if err := c.writeDir(""); err != nil {
		return err
	}
	return c.walk(ctx, "")
}

// walk descends the owner directories; a directory holding HEAD and objects/
// is a git dir, whatever it is called, so `.deleted.<unix>` copies are kept.
func (c *repoCopier) walk(ctx context.Context, rel string) error {
	entries, err := os.ReadDir(c.disk(rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := e.Name()
		if c.plain && strings.HasPrefix(name, ".tmp-") {
			continue
		}
		if !c.plain && rel == "" && (name == ".import-tmp" || strings.HasPrefix(name, ".readyz-")) {
			continue
		}
		child := path.Join(rel, name)
		switch {
		case e.IsDir() && !c.plain && isGitDir(c.disk(child)):
			err = c.copyGitDir(ctx, child)
		case e.IsDir():
			if err = c.writeDir(child); err == nil {
				err = c.walk(ctx, child)
			}
		case e.Type().IsRegular():
			err = c.copyFile(child, false)
		default:
			slog.Warn("backup: skipping entry that is neither a file nor a directory", "path", c.disk(child))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *repoCopier) copyGitDir(ctx context.Context, rel string) error {
	if err := c.writeDir(rel); err != nil {
		return err
	}
	refFiles := []string{"HEAD", "config", "packed-refs"}
	for _, name := range refFiles {
		if err := c.copyFile(path.Join(rel, name), true); err != nil {
			return err
		}
	}
	if err := c.copyTree(ctx, path.Join(rel, "refs"), true); err != nil {
		return err
	}
	if c.afterRefs != nil {
		c.afterRefs(c.disk(rel))
	}

	entries, err := os.ReadDir(c.disk(rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == "objects" || name == "refs" || slices.Contains(refFiles, name) || isGitNoise(name) {
			continue
		}
		if err := c.copyEntry(ctx, path.Join(rel, name), e, false); err != nil {
			return err
		}
	}
	return c.copyTree(ctx, path.Join(rel, "objects"), false)
}

// copyTree copies rel and everything under it. In objects/, pack/ goes last:
// a repack writes the new pack before it deletes the loose objects, so loose
// files listed first and packs listed last can't both miss an object.
func (c *repoCopier) copyTree(ctx context.Context, rel string, mem bool) error {
	if err := c.writeDir(rel); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	entries, err := os.ReadDir(c.disk(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var last []fs.DirEntry
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if isGitNoise(e.Name()) {
			continue
		}
		if e.Name() == "pack" {
			last = append(last, e)
			continue
		}
		if err := c.copyEntry(ctx, path.Join(rel, e.Name()), e, mem); err != nil {
			return err
		}
	}
	for _, e := range last {
		if err := c.copyEntry(ctx, path.Join(rel, e.Name()), e, mem); err != nil {
			return err
		}
	}
	return nil
}

func (c *repoCopier) copyEntry(ctx context.Context, rel string, e fs.DirEntry, mem bool) error {
	switch {
	case e.IsDir():
		return c.copyTree(ctx, rel, mem)
	case e.Type().IsRegular():
		return c.copyFile(rel, mem)
	}
	slog.Warn("backup: skipping entry that is neither a file nor a directory", "path", c.disk(rel))
	return nil
}

// copyFile skips a file that vanished since the directory was listed: a lock
// or a loose object being replaced. mem reads the whole file first, for the
// small ref files that a push rewrites in place.
func (c *repoCopier) copyFile(rel string, mem bool) error {
	f, err := os.Open(c.disk(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	size := info.Size()
	var data []byte
	if mem {
		if data, err = io.ReadAll(f); err != nil {
			return err
		}
		size = int64(len(data))
	}
	hdr := &tar.Header{Name: path.Join(c.prefix, rel), Typeflag: tar.TypeReg, Size: size, Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()}
	if err := c.tw.WriteHeader(hdr); err != nil {
		return err
	}
	if mem {
		_, err = c.tw.Write(data)
	} else if _, err = io.CopyN(c.tw, f, size); errors.Is(err, io.EOF) {
		err = fmt.Errorf("%s shrank while being copied", c.disk(rel))
	}
	if err != nil {
		return err
	}
	c.stats.Files++
	c.stats.Bytes += size
	return nil
}

func (c *repoCopier) writeDir(rel string) error {
	name := c.prefix + "/"
	if rel != "" {
		name = path.Join(c.prefix, rel) + "/"
	}
	info, err := os.Stat(c.disk(rel))
	if err != nil {
		return err
	}
	return c.tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()})
}

func (c *repoCopier) disk(rel string) string { return filepath.Join(c.root, filepath.FromSlash(rel)) }

func isGitDir(dir string) bool {
	head, err := os.Stat(filepath.Join(dir, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return false
	}
	objects, err := os.Stat(filepath.Join(dir, "objects"))
	return err == nil && objects.IsDir()
}

// Lock files and temp packs are half-written state that a restore must not see.
func isGitNoise(name string) bool {
	return strings.HasSuffix(name, ".lock") || strings.HasPrefix(name, "tmp_")
}
