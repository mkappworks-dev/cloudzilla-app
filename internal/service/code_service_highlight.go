package service

import (
	"html/template"
	"io"
	"log/slog"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
)

const (
	diffHighlightBytes = 4 << 20
	diffHighlightTime  = 2 * time.Second
)

// HighlightDiffs fills DiffLine.HTML for files. It highlights each side's
// whole blob, since a hunk can begin inside a block comment or string. It is
// opt-in because GetCommit and GetPullDiff also serve post-receive stats and
// CODEOWNERS lookups, which must not pay for it.
func (s *CodeService) HighlightDiffs(owner, repoName string, files []FileDiff) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		slog.Warn("highlight diffs: open repo failed", "owner", owner, "repo", repoName, "error", err)
		return
	}
	budget := highlight.NewBudget(diffHighlightBytes, diffHighlightTime)
	for i := range files {
		f := &files[i]
		if f.IsBinary || len(f.Hunks) == 0 {
			continue
		}
		oldSide := highlightBlob(repo, budget, f.OldPath, f.oldBlob)
		newSide := highlightBlob(repo, budget, f.NewPath, f.newBlob)
		for h := range f.Hunks {
			for l := range f.Hunks[h].Lines {
				line := &f.Hunks[h].Lines[l]
				if line.Type == "del" {
					line.HTML = oldSide.at(line.OldNum, line.Content)
				} else {
					line.HTML = newSide.at(line.NewNum, line.Content)
				}
			}
		}
	}
}

type highlightedBlob struct {
	src  []string
	html []template.HTML
}

// at returns line num's HTML only when the blob's line is text, so a diff
// that disagrees with the blob renders plain rather than showing other code.
func (b highlightedBlob) at(num int, text string) template.HTML {
	if num < 1 || num > len(b.html) || b.src[num-1] != text {
		return ""
	}
	return b.html[num-1]
}

func highlightBlob(repo *gogit.Repository, budget *highlight.Budget, path string, hash plumbing.Hash) highlightedBlob {
	if hash.IsZero() {
		return highlightedBlob{}
	}
	blob, err := repo.BlobObject(hash)
	if err != nil {
		slog.Warn("highlight diffs: read blob failed", "path", path, "blob", hash, "error", err)
		return highlightedBlob{}
	}
	if blob.Size > highlight.MaxBytes {
		return highlightedBlob{}
	}
	r, err := blob.Reader()
	if err != nil {
		slog.Warn("highlight diffs: open blob failed", "path", path, "blob", hash, "error", err)
		return highlightedBlob{}
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		slog.Warn("highlight diffs: read blob failed", "path", path, "blob", hash, "error", err)
		return highlightedBlob{}
	}
	src := string(data)
	html := highlight.LinesWithin(budget, path, src)
	if html == nil {
		return highlightedBlob{}
	}
	return highlightedBlob{src: strings.Split(src, "\n"), html: html}
}
