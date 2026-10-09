package markdown

import (
	"context"
	"regexp"
	"strconv"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// The leading class keeps a#3, &#38;, /x/#3 and ## from reading as references.
var refRe = regexp.MustCompile(`(^|[^\w&/#])#(\d+)\b`)

// RefNumbers returns the distinct #N numbers in src, in first-seen order. It
// scans raw text, so it can over-report numbers inside code; callers only use
// it to bound a lookup.
func RefNumbers(src string) []int {
	var out []int
	seen := map[int]bool{}
	for _, m := range refRe.FindAllStringSubmatch(src, -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// RenderWithRefs is RenderCtx plus links from #N to repoBase/kind/N for every N
// that kinds maps to "issues" or "pulls". Code, code blocks and existing links
// are left alone.
func RenderWithRefs(ctx context.Context, src, repoBase string, kinds map[int]string) string {
	return render(ctx, src, parser.WithASTTransformers(
		util.Prioritized[parser.ASTTransformer](&refLinker{base: repoBase, kinds: kinds}, 998),
	))
}

type refLinker struct {
	base  string
	kinds map[int]string
}

func (t *refLinker) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var targets []*ast.Text
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.(type) {
		case *ast.Link, *ast.AutoLink, *ast.Image, *ast.CodeSpan:
			return ast.WalkSkipChildren, nil
		}
		if tn, ok := n.(*ast.Text); ok && entering {
			targets = append(targets, tn)
		}
		return ast.WalkContinue, nil
	})
	for _, tn := range targets {
		t.split(tn, tn.Value.Str(source))
	}
}

// split replaces tn with text and link nodes when s holds a known reference.
func (t *refLinker) split(tn *ast.Text, s string) {
	parent := tn.Parent()
	if parent == nil {
		return
	}
	matches := refRe.FindAllStringSubmatchIndex(s, -1)
	var pieces []ast.Node
	last := 0
	for _, m := range matches {
		num, err := strconv.Atoi(s[m[4]:m[5]])
		kind := t.kinds[num]
		if err != nil || kind == "" {
			continue
		}
		hash := m[4] - 1
		if hash > last {
			pieces = append(pieces, ownedText(s[last:hash]))
		}
		link := ast.NewLink(text.NewSingleLineValueFromString(t.base+"/"+kind+"/"+strconv.Itoa(num), text.IdentityDecoder))
		link.AppendChild(ownedText(s[hash:m[5]]))
		pieces = append(pieces, link)
		last = m[5]
	}
	if len(pieces) == 0 {
		return
	}
	if last < len(s) {
		pieces = append(pieces, ownedText(s[last:]))
	}
	if tail, ok := pieces[len(pieces)-1].(*ast.Text); ok {
		tail.SetSoftLineBreak(tn.SoftLineBreak())
		tail.SetHardLineBreak(tn.HardLineBreak())
	} else {
		pieces = append(pieces, lineBreakOnly(tn))
	}
	for _, p := range pieces {
		parent.InsertBefore(tn, p)
	}
	parent.RemoveChild(tn)
}

func ownedText(s string) *ast.Text {
	return ast.NewText(text.NewSingleLineValueFromString(s, text.IdentityDecoder))
}

// lineBreakOnly carries tn's line-break flags when a link ends the segment.
func lineBreakOnly(tn *ast.Text) *ast.Text {
	t := ownedText("")
	t.SetSoftLineBreak(tn.SoftLineBreak())
	t.SetHardLineBreak(tn.HardLineBreak())
	return t
}
