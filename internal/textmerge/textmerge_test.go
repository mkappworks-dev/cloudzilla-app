package textmerge

import "testing"

func TestMerge(t *testing.T) {
	const theirsEnd = ">>>>>>> main (current)\n"
	tests := []struct {
		name               string
		base, ours, theirs string
		want               string
		wantConflicts      int
	}{
		{"identical", "a\nb\n", "a\nb\n", "a\nb\n", "a\nb\n", 0},
		{"only ours changed", "a\nb\n", "a\nB\n", "a\nb\n", "a\nB\n", 0},
		{"only theirs changed", "a\nb\n", "a\nb\n", "A\nb\n", "A\nb\n", 0},
		{"same change on both sides", "a\nb\n", "a\nX\n", "a\nX\n", "a\nX\n", 0},
		{"separate edits", "a\nb\nc\nd\ne\n", "A\nb\nc\nd\ne\n", "a\nb\nc\nd\nE\n", "A\nb\nc\nd\nE\n", 0},
		{"insert and edit apart", "a\nb\nc\nd\n", "a\nb\nc\nd\nz\n", "A\nb\nc\nd\n", "A\nb\nc\nd\nz\n", 0},
		{"adjacent edits conflict", "a\nb\nc\n", "a\nB\nc\n", "a\nb\nC\n",
			"a\n<<<<<<< your changes\nB\nc\n=======\nb\nC\n" + theirsEnd, 1},
		{"overlap", "a\nb\nc\n", "a\nmine\nc\n", "a\ntheirs\nc\n",
			"a\n<<<<<<< your changes\nmine\n=======\ntheirs\n" + theirsEnd + "c\n", 1},
		{"both insert at one point", "a\nc\n", "a\nmine\nc\n", "a\ntheirs\nc\n",
			"a\n<<<<<<< your changes\nmine\n=======\ntheirs\n" + theirsEnd + "c\n", 1},
		{"two separate conflicts", "a\nb\nc\nd\ne\n", "a\nX\nc\nY\ne\n", "a\nP\nc\nQ\ne\n",
			"a\n<<<<<<< your changes\nX\n=======\nP\n" + theirsEnd + "c\n<<<<<<< your changes\nY\n=======\nQ\n" + theirsEnd + "e\n", 2},
		{"theirs deletes a line ours left", "a\nb\nc\n", "a\nb\nc\n", "a\nc\n", "a\nc\n", 0},
		{"ours deletes a line theirs left", "a\nb\nc\n", "a\nc\n", "a\nb\nc\n", "a\nc\n", 0},
		{"ours deletes what theirs edits", "a\nb\nc\n", "a\nc\n", "a\nB\nc\n",
			"a\n<<<<<<< your changes\n=======\nB\n" + theirsEnd + "c\n", 1},
		{"both delete the same line", "a\nb\nc\n", "a\nc\n", "a\nc\n", "a\nc\n", 0},
		{"empty base", "", "mine\n", "theirs\n",
			"<<<<<<< your changes\nmine\n=======\ntheirs\n" + theirsEnd, 1},
		{"CRLF lines", "a\r\nb\r\nc\r\n", "A\r\nb\r\nc\r\n", "a\r\nb\r\nC\r\n", "A\r\nb\r\nC\r\n", 0},
		{"CRLF conflict keeps CRLF in the blocks", "a\r\nb\r\n", "a\r\nX\r\n", "a\r\nY\r\n",
			"a\r\n<<<<<<< your changes\nX\r\n=======\nY\r\n" + theirsEnd, 1},
		{"no trailing newline, theirs adds a line", "a\nb", "a\nb", "a\nb\nc", "a\nb\nc", 0},
		{"no trailing newline, edits apart", "a\nb\nc", "A\nb\nc", "a\nb\nC", "A\nb\nC", 0},
		{"no trailing newline conflict", "a\nb", "a\nX", "a\nY",
			"a\n<<<<<<< your changes\nX\n=======\nY\n" + theirsEnd, 1},
		{"file holding a divider merges", "a\n=======\nb\n", "A\n=======\nb\n", "a\n=======\nB\n", "A\n=======\nB\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Merge(tt.base, tt.ours, tt.theirs, "main")
			if got.Text != tt.want || got.Conflicts != tt.wantConflicts {
				t.Errorf("Merge = {%q, %d}, want {%q, %d}", got.Text, got.Conflicts, tt.want, tt.wantConflicts)
			}
		})
	}
}

func TestHasMarkers(t *testing.T) {
	tests := []struct {
		name, text string
		want       bool
	}{
		{"none", "a\nb\n", false},
		{"divider alone", "a\n=======\nb\n", false},
		{"our label", "a\n<<<<<<< your changes\nb\n", true},
		{"their label", "a\n>>>>>>> main (current)\n", true},
		{"their label with CRLF", "a\r\n>>>>>>> main (current)\r\n", true},
		{"other branch", "a\n>>>>>>> dev (current)\n", false},
		{"label inside a line", "x <<<<<<< your changes\n", false},
		{"git's own marker", "<<<<<<< HEAD\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasMarkers(tt.text, "main"); got != tt.want {
				t.Errorf("HasMarkers(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
