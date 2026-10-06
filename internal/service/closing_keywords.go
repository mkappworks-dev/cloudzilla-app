package service

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
)

// ClosingRef is an issue a closing keyword names. Owner and Repo are empty for
// a bare "#N", which means the text's own repo.
type ClosingRef struct {
	Owner  string
	Repo   string
	Number int
}

// \b is ASCII-only in RE2, which matches how git messages are written; the
// trailing \b rejects "#12abc" by backtracking into the digits and failing.
var closingRefRe = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+(?:([A-Za-z0-9][A-Za-z0-9_-]*)/([A-Za-z0-9][A-Za-z0-9._-]*))?#(\d+)\b`)

// ParseClosingRefs returns the issues text closes with keywords such as
// "Fixes #12" or "closes owner/repo#12", once each, in order of appearance.
func ParseClosingRefs(text string) []ClosingRef {
	var out []ClosingRef
	seen := map[string]bool{}
	for _, m := range closingRefRe.FindAllStringSubmatch(text, -1) {
		owner, repo := m[1], m[2]
		if owner != "" && (!ownerNameRe.MatchString(owner) || !validNameRe.MatchString(repo)) {
			continue
		}
		n, err := strconv.ParseInt(m[3], 10, 32)
		if err != nil || n <= 0 {
			continue
		}
		key := strings.ToLower(owner+"/"+repo) + "#" + strconv.FormatInt(n, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ClosingRef{Owner: owner, Repo: repo, Number: int(n)})
	}
	return out
}

// ClosingRefsInMerge returns the closing references in the messages of the
// commits merging headHash into base would bring in. Call it before the merge:
// a fast-forward leaves nothing between the two afterwards.
func (s *CodeService) ClosingRefsInMerge(owner, repoName, base string, headHash plumbing.Hash) ([]ClosingRef, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return nil, err
	}
	commits, err := commitRange(repo, baseCommit.Hash, headHash)
	if err != nil {
		return nil, err
	}
	var out []ClosingRef
	seen := map[ClosingRef]bool{}
	for i := len(commits) - 1; i >= 0; i-- {
		for _, ref := range ParseClosingRefs(commits[i].Message) {
			key := ClosingRef{Owner: strings.ToLower(ref.Owner), Repo: strings.ToLower(ref.Repo), Number: ref.Number}
			if !seen[key] {
				seen[key] = true
				out = append(out, ref)
			}
		}
	}
	return out, nil
}
