package markdown

import (
	"bytes"
	"html"
	"io"
	"log/slog"
	"strings"

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

// Render converts markdown src to safe HTML. Mermaid fenced blocks are
// wrapped in <pre class="mermaid"> for client-side rendering by mermaid.js.
// Render converts Markdown source to safe HTML, sanitizing links and disabling raw HTML.
func Render(src string) string {
	source := []byte(src)
	p := parser.New(
		parser.WithExtensions(extension.GFMParser),
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](&linkSanitizer{}, 999),
		),
	)
	r := ghtml.New(
		ghtml.WithHardWraps(),
		ghtml.WithExtensions(extension.GFMHTMLRenderer),
		ghtml.WithNodeRendererDecorator(ast.KindCodeBlock, renderMermaid),
	)
	var buf bytes.Buffer
	if err := r.Render(&buf, source, p.Parse(source)); err != nil {
		slog.Warn("markdown: failed to convert content, falling back to escaped source", "error", err)
		return html.EscapeString(src)
	}
	return buf.String()
}
