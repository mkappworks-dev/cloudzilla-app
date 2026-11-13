package runner

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
)

// Checkout puts the Job's commit in dir. It runs on the runner host with the per-job token, in the runner process, so
// no image has to carry git and no workflow code runs yet.
type Checkout func(ctx context.Context, spec *runnerapi.JobSpec, dir string) error

// GitCheckout fetches exactly spec.SHA over HTTP with the per-job token and checks it out.
func GitCheckout(ctx context.Context, spec *runnerapi.JobSpec, dir string) error {
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("init checkout: %w", err)
	}
	if _, err := repo.CreateRemote(&gogitconfig.RemoteConfig{Name: "origin", URLs: []string{spec.CloneURL}}); err != nil {
		return fmt.Errorf("add remote: %w", err)
	}
	auth := &githttp.BasicAuth{Username: "cz-job", Password: spec.Token}
	fetch := func(specs ...gogitconfig.RefSpec) error {
		err := repo.FetchContext(ctx, &gogit.FetchOptions{RemoteName: "origin", RefSpecs: specs, Auth: auth, Force: true})
		if err == gogit.NoErrAlreadyUpToDate {
			return nil
		}
		return err
	}
	// The commit may sit on a branch, a tag or a pull request head; a branch-and-tag fetch is the fallback when the
	// server will not serve a bare SHA.
	if err := fetch(gogitconfig.RefSpec("+" + spec.SHA + ":refs/cz/head")); err != nil {
		if err := fetch("+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"); err != nil {
			return fmt.Errorf("fetch %s: %w", spec.SHA, err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("open worktree: %w", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Hash: plumbing.NewHash(spec.SHA), Force: true}); err != nil {
		return fmt.Errorf("check out %s: %w", spec.SHA, err)
	}
	return nil
}

// tarWorkspace streams the workspace volume's initial content: the checkout under src/ and an empty home/. Entries are
// world-writable because docker cp ignores the archive's owner and makes everything root's, so mode is the only way a
// non-root step can write to its own checkout.
func tarWorkspace(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	if err := tw.WriteHeader(&tar.Header{Name: "home/", Typeflag: tar.TypeDir, Mode: 0o777}); err != nil {
		return err
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: filepath.ToSlash(filepath.Join("src", rel)), ModTime: info.ModTime()}
		switch {
		case d.IsDir():
			hdr.Typeflag, hdr.Mode = tar.TypeDir, 0o777
			hdr.Name += "/"
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr.Typeflag, hdr.Linkname, hdr.Mode = tar.TypeSymlink, target, 0o777
		case info.Mode().IsRegular():
			hdr.Typeflag, hdr.Size, hdr.Mode = tar.TypeReg, info.Size(), 0o666|int64(info.Mode().Perm()&0o111)
		default:
			return nil
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}
