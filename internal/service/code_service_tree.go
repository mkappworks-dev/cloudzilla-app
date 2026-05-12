package service

import (
	"errors"
	"html/template"
	"log/slog"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
)

// TreeEntry represents a single file or directory in a git tree.
type TreeEntry struct {
	Name  string
	IsDir bool
	URL   string
}

// TreeResult holds the directory listing returned by GetTree.
type TreeResult struct {
	Entries     []TreeEntry
	Ref         string
	Path        string
	Breadcrumbs []BreadcrumbPart
}

// BlobResult holds the content of a single file as numbered lines.
type BlobResult struct {
	Ref         string
	Path        string
	Lines       []CodeLine
	IsBinary    bool
	Breadcrumbs []BreadcrumbPart
	BlameURL    string
}

// GetTree returns tree entries for the given path (empty = root).
func (s *CodeService) GetTree(owner, repoName, ref, path string) (*TreeResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	rootTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var tree *object.Tree
	if path == "" {
		tree = rootTree
	} else {
		tree, err = rootTree.Tree(path)
		if err != nil {
			return nil, err
		}
	}

	var dirs, files []TreeEntry
	for _, entry := range tree.Entries {
		isDir := entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule
		var entryPath string
		if path == "" {
			entryPath = entry.Name
		} else {
			entryPath = path + "/" + entry.Name
		}
		var url string
		if isDir {
			url = "/" + owner + "/" + repoName + "/tree/" + displayRef + "/" + entryPath
		} else {
			url = "/" + owner + "/" + repoName + "/blob/" + displayRef + "/" + entryPath
		}
		e := TreeEntry{Name: entry.Name, IsDir: isDir, URL: url}
		if isDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	entries := append(dirs, files...)

	return &TreeResult{
		Entries:     entries,
		Ref:         displayRef,
		Path:        path,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, displayRef, path, false),
	}, nil
}

// GetBlob returns file content. Sets IsBinary=true for binary files.
func (s *CodeService) GetBlob(owner, repoName, ref, path string) (*BlobResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	file, err := commit.File(path)
	if err != nil {
		return nil, err
	}

	isBinary, err := file.IsBinary()
	if err != nil {
		return nil, err
	}

	result := &BlobResult{
		Ref:         displayRef,
		Path:        path,
		IsBinary:    isBinary,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, displayRef, path, true),
		BlameURL:    "/" + owner + "/" + repoName + "/blame/" + displayRef + "/" + path,
	}

	if !isBinary {
		contents, err := file.Contents()
		if err != nil {
			return nil, err
		}
		rawLines := strings.Split(contents, "\n")
		lines := make([]CodeLine, len(rawLines))
		for i, text := range rawLines {
			lines[i] = CodeLine{Num: i + 1, Text: text}
		}
		result.Lines = lines
	}

	return result, nil
}

// GetRawBlob returns the raw byte content of a non-binary file.
func (s *CodeService) GetRawBlob(owner, repoName, ref, path string) ([]byte, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}
	f, err := commit.File(path)
	if err != nil {
		return nil, err
	}
	isBinary, _ := f.IsBinary()
	if isBinary {
		return nil, errors.New("binary file")
	}
	contents, err := f.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(contents), nil
}

// GetProfileReadme reads README.md from the root of the given repo's default
// branch and renders it as HTML. Returns empty template.HTML when the repo,
// commit, or README does not exist. Unexpected infrastructure errors (corrupt
// repo, unreadable blob) are logged at warn level and also yield empty HTML.
func (s *CodeService) GetProfileReadme(ownerName, repoName, defaultBranch string) template.HTML {
	raw, err := s.GetRawBlob(ownerName, repoName, defaultBranch, "README.md")
	if err != nil {
		if !errors.Is(err, ErrEmptyRepo) && !errors.Is(err, object.ErrFileNotFound) {
			slog.Warn("profile readme: unexpected error reading README.md",
				"owner", ownerName,
				"repo", repoName,
				"branch", defaultBranch,
				"error", err,
			)
		}
		return template.HTML("")
	}
	return template.HTML(markdown.Render(string(raw)))
}
