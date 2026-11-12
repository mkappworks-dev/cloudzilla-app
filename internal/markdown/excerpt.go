package markdown

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
)

// ExcerptLines returns the visible text of Markdown source, one entry per heading, paragraph or list item, cut to at
// most maxRunes runes in total plus "…". Code blocks, raw HTML and image alt text are dropped: they read as noise in a
// preview. Keeping the blocks apart is what stops a heading and its bullets running together into one sentence.
func ExcerptLines(src string, maxRunes int) []string {
	source := []byte(src)
	doc := parser.New(parser.WithExtensions(extension.GFMParser)).Parse(source)
	var lines []string
	var cur strings.Builder
	written := 0
	flush := func() {
		if text := strings.Join(strings.Fields(cur.String()), " "); text != "" {
			lines = append(lines, text)
		}
		cur.Reset()
	}
	write := func(s string) {
		cur.WriteString(s)
		written += utf8.RuneCountInString(s)
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			switch n.(type) {
			case *ast.Paragraph, *ast.Heading, *ast.ListItem:
				flush()
			}
			return ast.WalkContinue, nil
		}
		// Bodies can be long; collapsing may shrink the text, so overshoot before stopping.
		if written > 2*maxRunes+64 {
			flush()
			return ast.WalkStop, nil
		}
		switch n := n.(type) {
		case *ast.ListItem:
			flush()
		case *ast.CodeBlock, *ast.HTMLBlock, *ast.RawHTML, *ast.Image:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			write(n.Value.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				write(" ")
			}
		case *ast.CodeSpan:
			write(n.Value.Value(source))
		case *ast.AutoLink:
			write(n.Label.Value(source))
		}
		return ast.WalkContinue, nil
	})
	return truncateLines(lines, maxRunes)
}

func truncateLines(lines []string, maxRunes int) []string {
	left := maxRunes
	for i, line := range lines {
		n := utf8.RuneCountInString(line)
		if n < left {
			left -= n
			continue
		}
		if n == left && i == len(lines)-1 {
			return lines
		}
		return append(lines[:i:i], strings.TrimRight(string([]rune(line)[:left]), " ")+"…")
	}
	return lines
}
