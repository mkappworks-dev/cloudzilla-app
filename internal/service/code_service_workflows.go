package service

import (
	"errors"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	WorkflowDir          = ".cloudzilla/workflows"
	maxWorkflowFiles     = 20
	maxWorkflowFileBytes = 1 << 20
)

// WorkflowFile is one file of a commit's workflows directory.
type WorkflowFile struct {
	Path    string
	Content []byte
	// Err is set when the file cannot be read as a workflow, such as one over the size cap.
	Err string
}

// WorkflowFilesAt reads the *.yml and *.yaml files directly under .cloudzilla/workflows at commit sha, by path.
// A commit without the directory has none. Files past the 20th are left out.
func (s *CodeService) WorkflowFilesAt(owner, repoName, sha string) ([]WorkflowFile, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	dir, err := tree.Tree(WorkflowDir)
	if errors.Is(err, object.ErrDirectoryNotFound) || errors.Is(err, object.ErrEntryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []WorkflowFile
	for _, e := range dir.Entries {
		ext := strings.ToLower(path.Ext(e.Name))
		if (ext != ".yml" && ext != ".yaml") || !e.Mode.IsFile() || e.Mode == filemode.Symlink {
			continue
		}
		f := WorkflowFile{Path: WorkflowDir + "/" + e.Name}
		blob, err := repo.BlobObject(e.Hash)
		if err != nil {
			return nil, err
		}
		if blob.Size > maxWorkflowFileBytes {
			f.Err = "workflow file is over 1 MiB"
		} else if f.Content, err = readWorkflowBlob(blob); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) > maxWorkflowFiles {
		out = out[:maxWorkflowFiles]
	}
	return out, nil
}

func readWorkflowBlob(b *object.Blob) ([]byte, error) {
	r, err := b.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}
