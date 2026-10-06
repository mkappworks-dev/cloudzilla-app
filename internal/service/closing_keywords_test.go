package service

import (
	"reflect"
	"testing"
)

func TestParseClosingRefs(t *testing.T) {
	bare := func(n int) ClosingRef { return ClosingRef{Number: n} }
	cases := []struct {
		name string
		text string
		want []ClosingRef
	}{
		{"close", "close #1", []ClosingRef{bare(1)}},
		{"closes", "Closes #2", []ClosingRef{bare(2)}},
		{"closed", "CLOSED #3", []ClosingRef{bare(3)}},
		{"fix", "fix #4", []ClosingRef{bare(4)}},
		{"fixes", "FiXeS #5", []ClosingRef{bare(5)}},
		{"fixed", "Fixed #6", []ClosingRef{bare(6)}},
		{"resolve", "resolve #7", []ClosingRef{bare(7)}},
		{"resolves", "Resolves #8", []ClosingRef{bare(8)}},
		{"resolved", "RESOLVED #9", []ClosingRef{bare(9)}},
		{"colon", "fixes: #10", []ClosingRef{bare(10)}},
		{"newline whitespace", "Fixes\n#11", []ClosingRef{bare(11)}},
		{"in a sentence", "This PR fixes #12.", []ClosingRef{bare(12)}},
		{"cross repo", "Fixes acme/api#13", []ClosingRef{{Owner: "acme", Repo: "api", Number: 13}}},
		{"cross repo with dots", "closes my-org/api.v2#14", []ClosingRef{{Owner: "my-org", Repo: "api.v2", Number: 14}}},
		{"two keywords", "Fixes #1, fixes #2", []ClosingRef{bare(1), bare(2)}},
		{"one keyword per ref", "Fixes #1, #2", []ClosingRef{bare(1)}},
		{"duplicates collapse", "fixes #3 and closes #3 and resolves #1", []ClosingRef{bare(3), bare(1)}},
		{"cross repo duplicates collapse case-insensitively", "fixes Acme/API#3, closes acme/api#3", []ClosingRef{{Owner: "Acme", Repo: "API", Number: 3}}},
		{"prefix word", "prefixes #1", nil},
		{"no space", "Fixes#1", nil},
		{"trailing word chars", "Fixes #1abc", nil},
		{"zero", "Fixes #0", nil},
		{"out of int32 range", "Fixes #2147483648", nil},
		{"max int32", "Fixes #2147483647", []ClosingRef{bare(2147483647)}},
		{"bad owner", "Fixes -acme/api#1", nil},
		{"too many slashes", "Fixes a/b/c#1", nil},
		{"no keyword", "See #1", nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseClosingRefs(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseClosingRefs(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
