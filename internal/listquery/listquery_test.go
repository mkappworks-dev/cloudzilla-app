package listquery

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Query
	}{
		{"empty", "", Query{}},
		{"free text", "fix  crash", Query{Text: "fix crash"}},
		{"is", "is:open", Query{State: "open"}},
		{"is case-insensitive", "IS:Merged", Query{State: "merged"}},
		{"later is wins", "is:open is:closed", Query{State: "closed"}},
		{"bad is stays text", "is:bogus", Query{Text: "is:bogus"}},
		{"author", "author:priya", Query{Author: "priya"}},
		{"author me", "author:@me", Query{Author: "@me"}},
		{"assignee", "assignee:@me", Query{Assignee: "@me"}},
		{"milestone", "milestone:v0.3", Query{Milestone: "v0.3"}},
		{"repeated labels sorted and deduped", "label:ui label:Bug label:BUG", Query{Labels: []string{"Bug", "ui"}}},
		{"quoted label", `label:"good first issue"`, Query{Labels: []string{"good first issue"}}},
		{"quoted free text", `"fix crash" now`, Query{Text: "fix crash now"}},
		{"no", "no:milestone no:label no:label", Query{No: []string{"label", "milestone"}}},
		{"bad no stays text", "no:author", Query{Text: "no:author"}},
		{"sort", "sort:updated", Query{Sort: "updated"}},
		{"default sort is dropped", "sort:newest", Query{}},
		{"review", "review:Changes_Requested", Query{Review: "changes_requested"}},
		{"reviewer", "reviewer:@me", Query{Reviewer: "@me"}},
		{"mentions", "mentions:@me", Query{Mentions: "@me"}},
		{"repo", "repo:acme/api", Query{Repo: "acme/api"}},
		{"bad review stays text", "review:maybe", Query{Text: "review:maybe"}},
		{"bad sort stays text", "sort:random", Query{Text: "sort:random"}},
		{"unknown key stays text", "foo:bar", Query{Text: "foo:bar"}},
		{"empty value stays text", "label:", Query{Text: "label:"}},
		{
			"everything",
			"crash is:open author:priya label:ui label:bug milestone:v1 sort:comments no:assignee",
			Query{
				Text: "crash", State: "open", Author: "priya", Labels: []string{"bug", "ui"},
				Milestone: "v1", Sort: "comments", No: []string{"assignee"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.raw); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestStringCanonicalOrder(t *testing.T) {
	q := Parse("crash sort:updated label:ui author:priya is:open label:bug")
	want := "is:open author:priya label:bug label:ui sort:updated crash"
	if got := q.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStringQuotesSpaces(t *testing.T) {
	q := Query{Labels: []string{"good first issue"}}
	if got, want := q.String(), `label:"good first issue"`; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, raw := range []string{
		"",
		"fix crash",
		"is:closed author:@me label:ui label:bug milestone:v0.3 no:assignee review:approved sort:oldest login",
		`label:"good first issue" label:"help wanted" milestone:"Q4 2026"`,
		"foo:bar is:bogus",
	} {
		q := Parse(raw)
		if got := Parse(q.String()); !reflect.DeepEqual(got, q) {
			t.Errorf("round trip of %q: got %+v, want %+v", raw, got, q)
		}
	}
}

func TestWith(t *testing.T) {
	q := Parse("is:open label:bug")
	if got, want := q.With(KeyLabel, "ui").String(), "is:open label:bug label:ui"; got != want {
		t.Errorf("add label = %q, want %q", got, want)
	}
	if got, want := q.With(KeyIs, "closed").String(), "is:closed label:bug"; got != want {
		t.Errorf("replace is = %q, want %q", got, want)
	}
	if got, want := q.With(KeyLabel, "BUG").String(), "is:open label:bug"; got != want {
		t.Errorf("duplicate label = %q, want %q", got, want)
	}
	if q.String() != "is:open label:bug" {
		t.Errorf("With mutated the receiver: %q", q.String())
	}
}

func TestWithout(t *testing.T) {
	q := Parse("is:open label:bug label:ui author:priya crash")
	tests := []struct {
		key, value, want string
	}{
		{KeyLabel, "bug", "is:open author:priya label:ui crash"},
		{KeyLabel, "UI", "is:open author:priya label:bug crash"},
		{KeyAuthor, "", "is:open label:bug label:ui crash"},
		{KeyIs, "", "author:priya label:bug label:ui crash"},
		{KeyText, "", "is:open author:priya label:bug label:ui"},
	}
	for _, tt := range tests {
		if got := q.Without(tt.key, tt.value).String(); got != tt.want {
			t.Errorf("Without(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
		}
	}
	if q.String() != "is:open author:priya label:bug label:ui crash" {
		t.Errorf("Without mutated the receiver: %q", q.String())
	}
}

func TestHas(t *testing.T) {
	q := Parse("is:open label:bug no:milestone")
	for _, tt := range []struct {
		key, value string
		want       bool
	}{
		{KeyLabel, "BUG", true},
		{KeyLabel, "ui", false},
		{KeyIs, "open", true},
		{KeyIs, "closed", false},
		{KeyNo, "milestone", true},
		{KeyAuthor, "priya", false},
	} {
		if got := q.Has(tt.key, tt.value); got != tt.want {
			t.Errorf("Has(%q, %q) = %v, want %v", tt.key, tt.value, got, tt.want)
		}
	}
}

func TestHasFilters(t *testing.T) {
	for raw, want := range map[string]bool{
		"":                    false,
		"is:open sort:oldest": false,
		"crash":               true,
		"author:priya":        true,
		"label:bug":           true,
		"milestone:v1":        true,
		"assignee:@me":        true,
		"no:label":            true,
	} {
		if got := Parse(raw).HasFilters(); got != want {
			t.Errorf("HasFilters(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestFromValuesLegacyAliases(t *testing.T) {
	v := url.Values{
		"state":     {"closed"},
		"label":     {"bug"},
		"milestone": {"12"},
		"sort":      {"recently-updated"},
		"q":         {"crash"},
	}
	got := FromValues(v)
	want := Query{Text: "crash", State: "closed", Labels: []string{"bug"}, MilestoneID: 12, Sort: "updated"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromValues = %+v, want %+v", got, want)
	}
}

func TestFromValuesQWins(t *testing.T) {
	v := url.Values{"q": {"is:open sort:oldest milestone:v1"}, "state": {"closed"}, "sort": {"newest"}, "milestone": {"3"}}
	got := FromValues(v)
	want := Query{State: "open", Sort: "oldest", Milestone: "v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromValues = %+v, want %+v", got, want)
	}
}

func TestFromValuesIgnoresBadLegacyValues(t *testing.T) {
	v := url.Values{"state": {"nonsense"}, "milestone": {"abc"}, "sort": {"random"}}
	if got := FromValues(v); !reflect.DeepEqual(got, Query{}) {
		t.Errorf("FromValues = %+v, want zero", got)
	}
}

func TestWithInvalidReturnsReceiver(t *testing.T) {
	q := Parse("is:open label:bug")
	for _, tt := range []struct{ key, value string }{
		{KeyIs, "bogus"}, {KeyNo, "author"}, {KeyLabel, ""}, {KeyText, "foo"}, {"nope", "x"},
	} {
		if got := q.With(tt.key, tt.value); !reflect.DeepEqual(got, q) {
			t.Errorf("With(%q, %q) = %+v, want unchanged", tt.key, tt.value, got)
		}
	}
}

func TestWithStripsQuotes(t *testing.T) {
	got := Parse("crash").With(KeyLabel, `5" display`)
	want := Query{Text: "crash", Labels: []string{"5 display"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("With = %+v, want %+v", got, want)
	}
}

func TestFromValuesQLabelsWinOverLegacy(t *testing.T) {
	got := FromValues(url.Values{"q": {"label:ui"}, "label": {"bug"}})
	if want := (Query{Labels: []string{"ui"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("FromValues = %+v, want %+v", got, want)
	}
}

func TestToken(t *testing.T) {
	if got, want := Token(KeyLabel, "good first issue"), `label:"good first issue"`; got != want {
		t.Errorf("Token = %q, want %q", got, want)
	}
	if got := Parse(Token(KeyLabel, "good first issue")); !got.Has(KeyLabel, "good first issue") {
		t.Errorf("Token does not parse back: %+v", got)
	}
}

func TestUnrecognized(t *testing.T) {
	q := Parse("is:open sort:comments-desc review:maybe crash std::vector 12:30 https://x.test/a note:")
	got := q.Unrecognized()
	if len(got) != 2 {
		t.Fatalf("want 2 notices, got %q", got)
	}
	if !strings.Contains(got[0], "sort: takes newest, oldest, updated, comments") {
		t.Errorf("sort hint missing: %q", got[0])
	}
	if !strings.Contains(got[1], "review:maybe") {
		t.Errorf("review token missing: %q", got[1])
	}
	if n := Parse("is:open label:bug fix").Unrecognized(); n != nil {
		t.Errorf("valid query has notices: %q", n)
	}
}

func TestAccountQualifiersRoundTrip(t *testing.T) {
	q := Parse("repo:acme/api reviewer:@me mentions:@me is:closed crash")
	want := "is:closed reviewer:@me mentions:@me repo:acme/api crash"
	if got := q.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(Parse(q.String()), q) {
		t.Errorf("String does not round-trip: %+v", q)
	}
	if !q.Has(KeyRepo, "ACME/api") || !q.Has(KeyReviewer, "@me") || !q.Has(KeyMentions, "@me") {
		t.Errorf("Has misses a qualifier in %+v", q)
	}
	got := q.Without(KeyRepo, "").Without(KeyReviewer, "").Without(KeyMentions, "")
	if got.Repo != "" || got.Reviewer != "" || got.Mentions != "" || !got.HasFilters() {
		t.Errorf("Without left %+v", got)
	}
	if !(Query{Repo: "a/b"}).HasFilters() {
		t.Error("repo must count as a filter")
	}
}
