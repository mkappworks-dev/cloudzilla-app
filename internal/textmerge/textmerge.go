// Package textmerge is a line-based three-way merge: go-git has none.
package textmerge

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	oursMarker   = "<<<<<<< your changes"
	divider      = "======="
	theirsMarker = ">>>>>>> "
)

// Result is a merged text and how many regions of it are conflict blocks.
type Result struct {
	Text      string
	Conflicts int
}

// TheirsLabel is the closing marker line for the changes already on branch.
func TheirsLabel(branch string) string { return theirsMarker + branch + " (current)" }

// HasMarkers reports whether text still holds one of Merge's own label lines
// for branch. The divider is not checked, so a file may contain "=======".
func HasMarkers(text, branch string) bool {
	theirs := TheirsLabel(branch)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == oursMarker || line == theirs {
			return true
		}
	}
	return false
}

// Merge applies the changes from base to ours and from base to theirs onto
// base. Changes that touch or overlap, and differ, become a conflict block of
// ours then theirs; branch names theirs in the closing marker.
func Merge(base, ours, theirs, branch string) Result {
	baseLines := splitLines(base)
	a := hunks(baseLines, splitLines(ours))
	b := hunks(baseLines, splitLines(theirs))

	var out strings.Builder
	var res Result
	pos := 0
	emit := func(lines []string) {
		for _, l := range lines {
			out.WriteString(l)
		}
	}
	for len(a) > 0 || len(b) > 0 {
		start, end, ha, hb := nextGroup(&a, &b)
		emit(baseLines[pos:start])
		pos = end
		ours, theirs := apply(baseLines, start, end, ha), apply(baseLines, start, end, hb)
		switch {
		case len(hb) == 0 || equal(ours, theirs):
			emit(ours)
		case len(ha) == 0:
			emit(theirs)
		default:
			res.Conflicts++
			out.WriteString(oursMarker + "\n")
			emit(ours)
			terminate(&out, ours)
			out.WriteString(divider + "\n")
			emit(theirs)
			terminate(&out, theirs)
			out.WriteString(TheirsLabel(branch) + "\n")
		}
	}
	emit(baseLines[pos:])
	res.Text = out.String()
	return res
}

// hunk replaces base[start:end] with lines.
type hunk struct {
	start, end int
	lines      []string
}

// splitLines keeps each line's terminator so a missing final newline survives.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// hunks diffs base against side by whole lines.
func hunks(base, side []string) []hunk {
	dmp := diffmatchpatch.New()
	a, b, table := dmp.DiffLinesToChars(strings.Join(base, ""), strings.Join(side, ""))
	var out []hunk
	var cur *hunk
	bi := 0
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, d := range dmp.DiffMain(a, b, false) {
		lines := splitLines(strings.Join(charsToLines(d.Text, table), ""))
		n := len([]rune(d.Text))
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			bi += n
		case diffmatchpatch.DiffDelete:
			if cur == nil {
				cur = &hunk{start: bi, end: bi}
			}
			cur.end += n
			bi += n
		case diffmatchpatch.DiffInsert:
			if cur == nil {
				cur = &hunk{start: bi, end: bi}
			}
			cur.lines = append(cur.lines, lines...)
		}
	}
	flush()
	return out
}

func charsToLines(chars string, table []string) []string {
	var out []string
	for _, r := range chars {
		out = append(out, table[r])
	}
	return out
}

// nextGroup pops the earliest run of hunks from a and b that touch one
// another, and returns the base span they cover with each side's share.
func nextGroup(a, b *[]hunk) (start, end int, ha, hb []hunk) {
	switch {
	case len(*a) == 0:
		start, end = (*b)[0].start, (*b)[0].end
	case len(*b) == 0 || (*a)[0].start <= (*b)[0].start:
		start, end = (*a)[0].start, (*a)[0].end
	default:
		start, end = (*b)[0].start, (*b)[0].end
	}
	for {
		grew := false
		for len(*a) > 0 && (*a)[0].start <= end {
			end = max(end, (*a)[0].end)
			ha = append(ha, (*a)[0])
			*a = (*a)[1:]
			grew = true
		}
		for len(*b) > 0 && (*b)[0].start <= end {
			end = max(end, (*b)[0].end)
			hb = append(hb, (*b)[0])
			*b = (*b)[1:]
			grew = true
		}
		if !grew {
			return start, end, ha, hb
		}
	}
}

// apply is base[start:end] with hs, all inside it, applied.
func apply(base []string, start, end int, hs []hunk) []string {
	var out []string
	pos := start
	for _, h := range hs {
		out = append(out, base[pos:h.start]...)
		out = append(out, h.lines...)
		pos = h.end
	}
	return append(out, base[pos:end]...)
}

func equal(a, b []string) bool {
	return strings.Join(a, "") == strings.Join(b, "")
}

// terminate adds the newline a marker line needs after lines that end without one.
func terminate(out *strings.Builder, lines []string) {
	if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
		out.WriteString("\n")
	}
}
