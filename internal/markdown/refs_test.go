package markdown

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestRefNumbers(t *testing.T) {
	cases := []struct {
		name, src string
		want      []int
	}{
		{"distinct in order", "see #12, #7 and #12 again; a#3 and &#38; are not refs", []int{12, 7}},
		{"word suffix is not a ref", "#5abc", nil},
		{"start of text", "#4 first", []int{4}},
		{"path and heading marks", "/x/#9 ## 8", nil},
		{"out of int range", "#99999999999999999999", nil},
		{"above int32 dropped", "#2147483648", nil},
		{"int32 max kept", "#2147483647", []int{2147483647}},
		{"zero dropped", "#0", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RefNumbers(c.src)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("RefNumbers(%q) = %v, want %v", c.src, got, c.want)
			}
		})
	}
}

func TestRenderWithRefs(t *testing.T) {
	kinds := map[int]string{5: "issues", 6: "pulls"}
	cases := []struct{ name, src, want, notWant string }{
		{"issue", "fixes #5", `href="/o/r/issues/5"`, ""},
		{"pull", "see #6", `href="/o/r/pulls/6"`, ""},
		{"unknown number stays text", "see #99", "#99", "href=\"/o/r/issues/99\""},
		{"code span untouched", "run `#5`", "<code>#5</code>", `href="/o/r/issues/5"`},
		{"fenced code untouched", "```\n#5\n```", "#5", `href="/o/r/issues/5"`},
		{"existing link untouched", "[#5](https://example.com)", `href="https://example.com"`, `href="/o/r/issues/5"`},
		{"autolink untouched", "<https://example.com/#5>", `href="https://example.com/#5"`, `href="/o/r/issues/5"`},
		{"word prefix untouched", "a#5", "a#5", `href="/o/r/issues/5"`},
		{"word suffix untouched", "#5abc", "#5abc", `href="/o/r/issues/5"`},
		{"entity untouched", "&#38;", "&amp;", `href="/o/r/issues/`},
		{"inside emphasis", "**fixes #5**", `<strong>fixes <a href="/o/r/issues/5">#5</a></strong>`, ""},
		{"two refs", "#5 and #6", `<a href="/o/r/pulls/6">#6</a>`, ""},
		{"raw html stays disabled", "<b>#5</b>", "", "<b>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := RenderWithRefs(context.Background(), c.src, "/o/r", kinds)
			if !strings.Contains(out, c.want) {
				t.Errorf("output %q missing %q", out, c.want)
			}
			if c.notWant != "" && strings.Contains(out, c.notWant) {
				t.Errorf("output %q contains %q", out, c.notWant)
			}
		})
	}
}
