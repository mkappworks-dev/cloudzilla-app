package markdown

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
)

// Excerpt returns the visible text of Markdown source, whitespace-collapsed and cut to at most maxRunes runes plus
// "…". Code blocks, raw HTML and image alt text are dropped: they read as noise in a one-line preview.
func Excerpt(src string, maxRunes int) string {
	source := []byte(src)
	doc := parser.New(parser.WithExtensions(extension.GFMParser)).Parse(source)
	var b strings.Builder
	written := 0
	write := func(s string) {
		b.WriteString(s)
		written += utf8.RuneCountInString(s)
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		// Bodies can be long; collapsing may shrink the text, so overshoot before stopping.
		if written > 2*maxRunes+64 {
			return ast.WalkStop, nil
		}
		switch n := n.(type) {
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
		case ast.BlockNode:
			write(" ")
		}
		return ast.WalkContinue, nil
	})
	text := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return strings.TrimRight(string([]rune(text)[:maxRunes]), " ") + "…"
}
