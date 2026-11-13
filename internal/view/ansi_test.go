package view

import (
	"reflect"
	"testing"
)

func TestParseANSI(t *testing.T) {
	cases := []struct {
		name, in string
		want     []LogSpan
	}{
		{"plain", "ok  pkg", []LogSpan{{Text: "ok  pkg"}}},
		{"colour then reset", "\x1b[32mok\x1b[0m  pkg", []LogSpan{{Text: "ok", Class: "text-success"}, {Text: "  pkg"}}},
		{"bold red", "\x1b[1;31mFAIL\x1b[m", []LogSpan{{Text: "FAIL", Class: "text-destructive font-medium"}}},
		{"cursor codes dropped", "a\x1b[2Kb\x1b[1Ac", []LogSpan{{Text: "a"}, {Text: "b"}, {Text: "c"}}},
		{"stray escape removed", "a\x1bb", []LogSpan{{Text: "ab"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseANSI(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseANSI(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}
