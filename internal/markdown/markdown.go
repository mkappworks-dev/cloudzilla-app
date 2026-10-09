package markdown

import (
	"bytes"
	"context"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer"
	ghtml "github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// linkSanitizer is a goldmark AST transformer that rewrites links whose
// destination uses a dangerous scheme (javascript:, vbscript:, data:) to "#".
// It must run before rendering so the default goldmark HTML renderer emits the
// sanitised href without any additional logic.
type linkSanitizer struct{}

func (t *linkSanitizer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		// Destination.Value is entity- and backslash-decoded, so &#106;avascript: is caught too.
		if link, ok := n.(*ast.Link); ok {
			if hasDangerousScheme(link.Destination.Value(source)) {
				link.Destination = text.NewSingleLineValueFromString("#", text.IdentityDecoder)
			}
		}
		if img, ok := n.(*ast.Image); ok {
			if hasDangerousScheme(img.Destination.Value(source)) {
				img.Destination = text.NewSingleLineValueFromString("", text.IdentityDecoder)
			}
		}
		return ast.WalkContinue, nil
	})
}

func hasDangerousScheme(dest string) bool {
	lower := strings.ToLower(strings.TrimSpace(dest))
	for _, prefix := range []string{"javascript:", "vbscript:", "data:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func renderMermaid(next ghtml.NodeRenderer) ghtml.NodeRenderer {
	return ghtml.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
		n := node.(*ast.CodeBlock)
		if lang, _ := n.Language(source); lang != "mermaid" {
			return next.Render(w, source, node, entering, rc)
		}
		if entering {
			bw := w.(util.BufWriter)
			// Write errors stick to w, a util.ErrorBufWriter, and surface from Render.
			_, _ = bw.WriteString(`<pre class="mermaid">`)
			_, _ = n.Value.WriteTo(ghtml.ContextTextWriter(rc), source)
			_, _ = bw.WriteString("</pre>\n")
		}
		return ast.WalkSkipChildren, nil
	})
}

// Highlighting is capped per document because Render runs uncached on every
// view of user-written content; a request's budget from highlight.WithBudget
// caps a page of many documents on top.
const (
	highlightBytes = 256 << 10
	highlightTime  = 250 * time.Millisecond
)

// highlightCode renders fenced blocks in a known language as token spans. It
// writes the whole block on entering, so exit must not emit a second close. The
// choice is remembered because a highlight can fall back to plain at its budget.
func highlightCode(budget *highlight.Budget) ghtml.NodeRendererDecorator {
	highlighted := map[ast.Node]bool{}
	return func(next ghtml.NodeRenderer) ghtml.NodeRenderer {
		return ghtml.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
			if !entering {
				if highlighted[node] {
					return ast.WalkContinue, nil
				}
				return next.Render(w, source, node, entering, rc)
			}
			n := node.(*ast.CodeBlock)
			lang, ok := n.Language(source)
			if !ok || n.CodeBlockKind != ast.CodeBlockKindFenced {
				return next.Render(w, source, node, entering, rc)
			}
			code := highlight.BlockWithin(budget, lang, "", n.Value.Str(source))
			if code == "" {
				return next.Render(w, source, node, entering, rc)
			}
			highlighted[node] = true
			bw := w.(util.BufWriter)
			_, _ = bw.WriteString(`<pre class="hl"><code class="language-`)
			_, _ = ghtml.ContextTextWriter(rc).WriteString(lang)
			_, _ = bw.WriteString(`">`)
			_, _ = bw.WriteString(string(code))
			_, _ = bw.WriteString("</code></pre>\n")
			return ast.WalkSkipChildren, nil
		})
	}
}

// Render converts Markdown source to safe HTML, sanitizing links and disabling raw HTML.
// Mermaid fences become <pre class="mermaid"> for mermaid.js; fences in a known
// language are syntax-highlighted.
func Render(src string) string {
	return RenderCtx(context.Background(), src)
}

// RenderCtx is Render with highlighting also charged to ctx's highlight budget, if any.
func RenderCtx(ctx context.Context, src string) string {
	return render(ctx, src)
}

func render(ctx context.Context, src string, extra ...parser.Option) string {
	source := []byte(src)
	p := parser.New(append([]parser.Option{
		parser.WithExtensions(extension.GFMParser),
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](&linkSanitizer{}, 999),
		),
	}, extra...)...)
	r := ghtml.New(
		ghtml.WithHardWraps(),
		ghtml.WithExtensions(extension.GFMHTMLRenderer),
		ghtml.WithNodeRendererDecorator(ast.KindCodeBlock, highlightCode(highlight.BudgetFrom(ctx).Sub(highlightBytes, highlightTime))),
		ghtml.WithNodeRendererDecorator(ast.KindCodeBlock, renderMermaid),
	)
	var buf bytes.Buffer
	if err := r.Render(&buf, source, p.Parse(source)); err != nil {
		slog.Warn("markdown: failed to convert content, falling back to escaped source", "error", err)
		return html.EscapeString(src)
	}
	return buf.String()
}
