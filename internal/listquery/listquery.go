// Package listquery parses the search string of the issue and pull request lists into qualifiers.
package listquery

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	KeyIs        = "is"
	KeyAuthor    = "author"
	KeyAssignee  = "assignee"
	KeyLabel     = "label"
	KeyMilestone = "milestone"
	KeyNo        = "no"
	KeySort      = "sort"
	KeyText      = "text"
)

// Me is the author/assignee value that handlers resolve to the signed-in user.
const Me = "@me"

const defaultSort = "newest"

var (
	validStates = []string{"open", "closed", "draft", "merged"}
	validNo     = []string{KeyLabel, KeyMilestone, KeyAssignee}
	validSorts  = []string{"newest", "oldest", "updated", "comments"}
)

// Query is a parsed list search. Which states and sorts a list honours is up to its handler.
type Query struct {
	Text      string
	State     string
	Author    string
	Assignee  string
	Labels    []string
	Milestone string
	No        []string
	Sort      string

	// MilestoneID comes only from the legacy milestone= param, which names an ID, and String drops it: the caller must
	// resolve it to a title before building links.
	MilestoneID int64
}

// Parse never fails: a token that is not a known qualifier with a valid value stays free text.
func Parse(raw string) Query {
	var q Query
	var text []string
	for _, tok := range tokenize(raw) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok || val == "" || !q.set(strings.ToLower(key), val) {
			text = append(text, tok)
		}
	}
	q.Text = strings.Join(text, " ")
	q.normalize()
	return q
}

// FromValues reads q, then fills any qualifier q left unset from the legacy state, label, milestone and sort params.
func FromValues(v url.Values) Query {
	q := Parse(v.Get("q"))
	if q.State == "" {
		q.set(KeyIs, v.Get("state"))
	}
	if len(q.Labels) == 0 {
		for _, l := range v["label"] {
			q.set(KeyLabel, l)
		}
	}
	if q.Milestone == "" {
		if id, err := strconv.ParseInt(v.Get("milestone"), 10, 64); err == nil && id > 0 {
			q.MilestoneID = id
		}
	}
	if q.Sort == "" {
		sort := v.Get("sort")
		if sort == "recently-updated" {
			sort = "updated"
		}
		q.set(KeySort, sort)
	}
	q.normalize()
	return q
}

// String is canonical: equal filters give equal strings, so equal URLs.
func (q Query) String() string {
	var parts []string
	if q.State != "" {
		parts = append(parts, KeyIs+":"+q.State)
	}
	if q.Author != "" {
		parts = append(parts, KeyAuthor+":"+quote(q.Author))
	}
	if q.Assignee != "" {
		parts = append(parts, KeyAssignee+":"+quote(q.Assignee))
	}
	for _, l := range q.Labels {
		parts = append(parts, KeyLabel+":"+quote(l))
	}
	if q.Milestone != "" {
		parts = append(parts, KeyMilestone+":"+quote(q.Milestone))
	}
	for _, n := range q.No {
		parts = append(parts, KeyNo+":"+n)
	}
	if q.Sort != "" {
		parts = append(parts, KeySort+":"+q.Sort)
	}
	if q.Text != "" {
		parts = append(parts, q.Text)
	}
	return strings.Join(parts, " ")
}

// HasFilters reports whether q narrows the list beyond its state tab and sort.
func (q Query) HasFilters() bool {
	return q.Text != "" || q.Author != "" || q.Assignee != "" || len(q.Labels) > 0 || q.Milestone != "" ||
		q.MilestoneID != 0 || len(q.No) > 0
}

// With returns q with the qualifier added; a single-valued one replaces its current value. An invalid key or value
// returns q unchanged rather than leaking the token into the free text.
func (q Query) With(key, value string) Query {
	if added := Parse(key + ":" + quote(value)); added.Text != "" {
		return q
	}
	return Parse(q.String() + " " + key + ":" + quote(value))
}

// Without returns q with the qualifier removed. value picks which label or no: entry; the other keys ignore it.
func (q Query) Without(key, value string) Query {
	q.Labels = slices.Clone(q.Labels)
	q.No = slices.Clone(q.No)
	switch key {
	case KeyIs:
		q.State = ""
	case KeyAuthor:
		q.Author = ""
	case KeyAssignee:
		q.Assignee = ""
	case KeyLabel:
		q.Labels = without(q.Labels, value)
	case KeyMilestone:
		q.Milestone, q.MilestoneID = "", 0
	case KeyNo:
		q.No = without(q.No, value)
	case KeySort:
		q.Sort = ""
	case KeyText:
		q.Text = ""
	}
	return q
}

// Has reports whether q holds the qualifier; value is ignored for text and compared case-insensitively.
func (q Query) Has(key, value string) bool {
	switch key {
	case KeyIs:
		return q.State != "" && q.State == strings.ToLower(value)
	case KeyAuthor:
		return strings.EqualFold(q.Author, value)
	case KeyAssignee:
		return strings.EqualFold(q.Assignee, value)
	case KeyLabel:
		return containsFold(q.Labels, value)
	case KeyMilestone:
		return strings.EqualFold(q.Milestone, value)
	case KeyNo:
		return containsFold(q.No, value)
	case KeySort:
		return q.Sort != "" && q.Sort == strings.ToLower(value)
	case KeyText:
		return q.Text != ""
	}
	return false
}

// set applies one qualifier and reports whether it was valid.
func (q *Query) set(key, val string) bool {
	val = strings.TrimSpace(val)
	if val == "" {
		return false
	}
	switch key {
	case KeyIs:
		return setEnum(&q.State, validStates, val)
	case KeySort:
		if !setEnum(&q.Sort, validSorts, val) {
			return false
		}
		// The default sort is not a qualifier, so equal lists share one canonical string.
		if q.Sort == defaultSort {
			q.Sort = ""
		}
		return true
	case KeyAuthor:
		q.Author = val
	case KeyAssignee:
		q.Assignee = val
	case KeyMilestone:
		q.Milestone = val
	case KeyLabel:
		q.Labels = append(q.Labels, val)
	case KeyNo:
		v := strings.ToLower(val)
		if !slices.Contains(validNo, v) {
			return false
		}
		q.No = append(q.No, v)
	default:
		return false
	}
	return true
}

// normalize dedupes case-insensitively, keeping the first spelling, and sorts: AND filters have no order.
func (q *Query) normalize() {
	q.Labels = sortedUnique(q.Labels)
	q.No = sortedUnique(q.No)
}

func setEnum(dst *string, allowed []string, val string) bool {
	v := strings.ToLower(val)
	if !slices.Contains(allowed, v) {
		return false
	}
	*dst = v
	return true
}

func sortedUnique(s []string) []string {
	var out []string
	for _, v := range s {
		if !containsFold(out, v) {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out
}

func containsFold(s []string, v string) bool {
	return slices.ContainsFunc(s, func(e string) bool { return strings.EqualFold(e, v) })
}

func without(s []string, v string) []string {
	out := slices.DeleteFunc(s, func(e string) bool { return strings.EqualFold(e, v) })
	if len(out) == 0 {
		return nil
	}
	return out
}

// quote drops double quotes: the tokenizer has no escape syntax, so a value holding one could never round-trip. A label
// named 5" display therefore filters as "5 display".
func quote(v string) string {
	v = strings.ReplaceAll(v, `"`, "")
	if strings.ContainsFunc(v, unicode.IsSpace) {
		return `"` + v + `"`
	}
	return v
}

// tokenize splits on whitespace outside double quotes and drops the quotes, so a value can hold spaces but never a
// quote.
func tokenize(raw string) []string {
	var tokens []string
	var b strings.Builder
	inQuote := false
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for _, r := range raw {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return tokens
}
