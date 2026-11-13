package service

import (
	"html/template"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
)

// BlameLine represents one annotated line of blame output with commit metadata.
type BlameLine struct {
	LineNum    int
	Text       string
	HTML       template.HTML `json:"-"`
	Hash       string        // short SHA (7 chars)
	FullHash   string        // full SHA, retained for /commit/{sha} links
	Author     string        // email, as go-git reports it
	AuthorName string
	AuthorTime time.Time
	ShowMeta   bool
	// Signature is set on hunk-start lines only; the rest of a hunk renders no badge.
	Signature SignatureResult
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
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	commit, resolvedRef, err := resolveRef(repo, ref)
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
			AuthorName: l.AuthorName,
			AuthorTime: l.Date,
			ShowMeta:   showMeta,
		}
	}

	s.attachBlameSignatures(repo, lines)

	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	if html := highlight.Lines(path, strings.Join(texts, "\n")); html != nil {
		for i := range lines {
			lines[i].HTML = html[i]
		}
	}

	return &BlameResult{
		Ref:         resolvedRef,
		Path:        path,
		Lines:       lines,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, resolvedRef, path, true),
		BlobURL:     codeurl.Path(owner, repoName, "blob", resolvedRef, path),
	}, nil
}

// attachBlameSignatures verifies each distinct commit once, however many hunks or lines it owns.
func (s *CodeService) attachBlameSignatures(repo *gogit.Repository, lines []BlameLine) {
	seen := map[string]bool{}
	var commits []*object.Commit
	for _, l := range lines {
		if !l.ShowMeta || seen[l.FullHash] {
			continue
		}
		seen[l.FullHash] = true
		if c, err := repo.CommitObject(plumbing.NewHash(l.FullHash)); err == nil {
			commits = append(commits, c)
		}
	}
	results := s.verifySignatures(commits)
	for i := range lines {
		if lines[i].ShowMeta {
			lines[i].Signature = results[plumbing.NewHash(lines[i].FullHash)]
		}
	}
}
