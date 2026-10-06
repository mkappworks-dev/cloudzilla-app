package service

import (
	"regexp"
	"strconv"
	"strings"
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
