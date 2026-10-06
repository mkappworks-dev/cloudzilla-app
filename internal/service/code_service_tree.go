package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/binary"
	"github.com/go-git/go-git/v5/utils/ioutil"
	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
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
	Size        int64 // file size in bytes
	Breadcrumbs []BreadcrumbPart
	BlameURL    string
}

// GetTree returns tree entries for the given path (empty = root).
func (s *CodeService) GetTree(owner, repoName, ref, path string) (*TreeResult, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	commit, resolvedRef, err := resolveRef(repo, ref)
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
			url = codeurl.Path(owner, repoName, "tree", resolvedRef, entryPath)
		} else {
			url = codeurl.Path(owner, repoName, "blob", resolvedRef, entryPath)
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
		Ref:         resolvedRef,
		Path:        path,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, resolvedRef, path, false),
	}, nil
}

// GetBlob returns file content. Sets IsBinary=true for binary files.
func (s *CodeService) GetBlob(owner, repoName, ref, path string) (*BlobResult, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	commit, resolvedRef, err := resolveRef(repo, ref)
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
		Ref:         resolvedRef,
		Path:        path,
		IsBinary:    isBinary,
		Size:        file.Size,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, resolvedRef, path, true),
		BlameURL:    codeurl.Path(owner, repoName, "blame", resolvedRef, path),
	}

	if !isBinary {
		contents, err := file.Contents()
		if err != nil {
			return nil, err
		}
		rawLines := strings.Split(contents, "\n")
		html := highlight.Lines(path, contents)
		lines := make([]CodeLine, len(rawLines))
		for i, text := range rawLines {
			lines[i] = CodeLine{Num: i + 1, Text: text}
			if html != nil {
				lines[i].HTML = html[i]
			}
		}
		result.Lines = lines
	}

	return result, nil
}

var ErrBlobTooLarge = errors.New("blob exceeds size limit")

func (s *CodeService) GetRawBlob(owner, repoName, ref, path string) ([]byte, error) {
	return s.GetRawBlobBounded(owner, repoName, ref, path, 0)
}

func (s *CodeService) GetRawBlobBounded(owner, repoName, ref, path string, maxBytes int64) ([]byte, error) {
	repo, err := s.openRepo(owner, repoName)
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
	if maxBytes > 0 && f.Size > maxBytes {
		return nil, ErrBlobTooLarge
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

// binarySniffBytes is binary.IsBinary's window, so OpenRawBlob classifies a
// file the same way GetBlob does.
const binarySniffBytes = 8000

// RawBlob is a file's content at a ref, open for streaming. The caller must
// Close it.
type RawBlob struct {
	io.ReadCloser
	Size     int64
	IsBinary bool
}

// OpenRawBlob opens the file at path for streaming. A file over maxBytes fails
// with ErrBlobTooLarge before any content is read.
func (s *CodeService) OpenRawBlob(owner, repoName, ref, path string, maxBytes int64) (*RawBlob, error) {
	repo, err := s.openRepo(owner, repoName)
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
	if f.Size > maxBytes {
		return nil, ErrBlobTooLarge
	}
	rc, err := f.Reader()
	if err != nil {
		return nil, err
	}
	// Sniffing through the buffer that then serves the content reads the blob
	// once; File.IsBinary would open and inflate it a second time.
	br := bufio.NewReaderSize(rc, binarySniffBytes)
	head, err := br.Peek(binarySniffBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = rc.Close()
		return nil, err
	}
	isBinary, _ := binary.IsBinary(bytes.NewReader(head))
	return &RawBlob{ReadCloser: ioutil.NewReadCloser(br, rc), Size: f.Size, IsBinary: isBinary}, nil
}

// GetProfileReadme reads README.md from the root of the given repo's default
// branch and renders it as HTML. Returns empty template.HTML when the repo,
// commit, or README does not exist. Unexpected infrastructure errors (corrupt
// repo, unreadable blob) are logged at warn level and also yield empty HTML.
func (s *CodeService) GetProfileReadme(ctx context.Context, ownerName, repoName, defaultBranch string) template.HTML {
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
	return template.HTML(markdown.RenderCtx(ctx, string(raw)))
}

// GetProfileReadmeRaw returns ("", nil) for a missing repo, commit, or README so callers can render an empty editor.
func (s *CodeService) GetProfileReadmeRaw(ownerName, repoName, defaultBranch string) (string, error) {
	raw, err := s.GetRawBlob(ownerName, repoName, defaultBranch, "README.md")
	if err != nil {
		if errors.Is(err, ErrEmptyRepo) || errors.Is(err, object.ErrFileNotFound) || errors.Is(err, gogit.ErrRepositoryNotExists) {
			return "", nil
		}
		return "", err
	}
	return string(raw), nil
}
