package service

import (
	"time"

	gogit "github.com/go-git/go-git/v5"
)

// BlameLine represents one annotated line of blame output with commit metadata.
type BlameLine struct {
	LineNum    int
	Text       string
	Hash       string // short SHA (7 chars)
	FullHash   string // full SHA, retained for /commit/{sha} links
	Author     string
	AuthorTime time.Time
	ShowMeta   bool
}

// BlameResult holds per-line blame annotations for a file at a given ref.
type BlameResult struct {
	Ref         string
	Path        string
	Lines       []BlameLine
	Breadcrumbs []BreadcrumbPart
	BlobURL     string
}

// GetBlame returns per-line blame information.
func (s *CodeService) GetBlame(owner, repoName, ref, path string) (*BlameResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	blameResult, err := gogit.Blame(commit, path)
	if err != nil {
		return nil, err
	}

	lines := make([]BlameLine, len(blameResult.Lines))
	for i, l := range blameResult.Lines {
		fullHash := l.Hash.String()
		short := fullHash
		if len(short) > 7 {
			short = short[:7]
		}
		showMeta := i == 0 || blameResult.Lines[i].Hash != blameResult.Lines[i-1].Hash
		lines[i] = BlameLine{
			LineNum:    i + 1,
			Text:       l.Text,
			Hash:       short,
			FullHash:   fullHash,
			Author:     l.Author,
			AuthorTime: l.Date,
			ShowMeta:   showMeta,
		}
	}

	return &BlameResult{
		Ref:         displayRef,
		Path:        path,
		Lines:       lines,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, displayRef, path, true),
		BlobURL:     "/" + owner + "/" + repoName + "/blob/" + displayRef + "/" + path,
	}, nil
}
