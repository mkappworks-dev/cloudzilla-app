package model

import "strings"

// Marks around a matched term in a snippet. Not ** or <b>: code and Markdown contain both, and the view must tell a match
// from content before it escapes the text.
const (
	SnippetMarkStart = "@@CZ-MARK@@"
	SnippetMarkEnd   = "@@/CZ-MARK@@"
)

// CodeSearchResult represents a single file match from a code search query.
type CodeSearchResult struct {
	RepoID    int64   `db:"repo_id"`
	RepoName  string  `db:"repo_name"`
	OwnerName string  `db:"owner_name"`
	FilePath  string  `db:"file_path"`
	Snippet   string  `db:"snippet"`
	Rank      float64 `db:"rank"`
}

// SnippetPart is a run of snippet text, flagged when the query matched it.
type SnippetPart struct {
	Text  string
	Match bool
}

// SnippetParts splits Snippet at its match marks.
func (r CodeSearchResult) SnippetParts() []SnippetPart {
	var parts []SnippetPart
	rest := r.Snippet
	for rest != "" {
		before, after, found := strings.Cut(rest, SnippetMarkStart)
		if before != "" {
			parts = append(parts, SnippetPart{Text: before})
		}
		if !found {
			break
		}
		match, tail, _ := strings.Cut(after, SnippetMarkEnd)
		if match != "" {
			parts = append(parts, SnippetPart{Text: match, Match: true})
		}
		rest = tail
	}
	return parts
}
