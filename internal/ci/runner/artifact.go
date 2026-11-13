package runner

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/workflow"
)

// uploadArtifacts archives each `artifacts:` path out of the Job's container and posts it. Every entry is tried, and
// the error names the ones that failed.
func (e *Executor) uploadArtifacts(ctx context.Context, d docker, spec *runnerapi.JobSpec, container string) error {
	var failed []string
	for _, a := range spec.Artifacts {
		if err := e.uploadArtifact(ctx, d, spec, container, a); err != nil {
			failed = append(failed, fmt.Sprintf("artifact %q: %v", a.Name, err))
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

func (e *Executor) uploadArtifact(ctx context.Context, d docker, spec *runnerapi.JobSpec, container string, a runnerapi.ArtifactSpec) error {
	if strings.TrimSpace(a.Path) == "" {
		return errors.New("path is empty")
	}
	src, err := workdir(a.Path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "cz-runner-artifact-*.zip")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	cp := d.cmd(ctx, "cp", container+":"+src, "-")
	var stderr bytes.Buffer
	cp.Stderr = &stderr
	stdout, err := cp.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cp.Start(); err != nil {
		return err
	}
	zerr := zipTar(stdout, tmp)
	_, _ = io.Copy(io.Discard, stdout)
	if err := cp.Wait(); err != nil {
		return fmt.Errorf("%s does not exist in the workspace (%s)", a.Path, strings.TrimSpace(trimOut(stderr.Bytes())))
	}
	if zerr != nil {
		return zerr
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// Without Secrets in scope: a name is shown in the UI.
	name, err := workflow.Interpolate(a.Name, workflow.Context{GitHub: spec.GitHub, Matrix: spec.Matrix, Needs: spec.Needs, Status: "success"})
	if err != nil {
		return fmt.Errorf("name: %w", err)
	}
	return e.API.UploadArtifact(ctx, spec.JobID, spec.Token, name, size, tmp)
}

// zipTar rewrites the tar stream of `docker cp` as a zip. Only directories and regular files are kept: a symlink in an
// artifact would point out of the archive when it is unpacked.
func zipTar(r io.Reader, w io.Writer) error {
	zw := zip.NewWriter(w)
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		name := path.Clean(h.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if _, err := zw.Create(name + "/"); err != nil {
				return err
			}
		case tar.TypeReg:
			hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: h.ModTime}
			hdr.SetMode(h.FileInfo().Mode())
			fw, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			if _, err := io.Copy(fw, tr); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}
