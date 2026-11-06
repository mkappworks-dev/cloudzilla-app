package components

import "testing"

func TestPluralize(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "repositories"},
		{1, "repository"},
		{2, "repositories"},
		{-1, "repositories"},
	} {
		if got := Pluralize(tc.n, "repository", "repositories"); got != tc.want {
			t.Errorf("Pluralize(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestCount(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0 entries"},
		{1, "1 entry"},
		{12, "12 entries"},
	} {
		if got := Count(tc.n, "entry", "entries"); got != tc.want {
			t.Errorf("Count(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
