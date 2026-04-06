package markdown

import (
	"bytes"
	"html"
	"html/template"
	"log/slog"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// linkSanitizer is a goldmark AST transformer that rewrites links whose
// destination uses a dangerous scheme (javascript:, vbscript:, data:) to "#".
// It must run before rendering so the default goldmark HTML renderer emits the
// sanitised href without any additional logic.
type linkSanitizer struct{}

func (t *linkSanitizer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if link, ok := n.(*ast.Link); ok {
			if hasDangerousScheme(link.Destination) {
				link.Destination = []byte("#")
			}
		}
		if img, ok := n.(*ast.Image); ok {
			if hasDangerousScheme(img.Destination) {
				img.Destination = []byte("")
			}
		}
		return ast.WalkContinue, nil
	})
}

func hasDangerousScheme(dest []byte) bool {
	lower := bytes.ToLower(bytes.TrimSpace(dest))
	for _, prefix := range [][]byte{
		[]byte("javascript:"),
		[]byte("vbscript:"),
		[]byte("data:"),
	} {
		if bytes.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

type mermaidRenderer struct{}

func (r *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCode)
}

func (r *mermaidRenderer) renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	lang := string(n.Language(source))

	var buf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(source))
	}

	if lang == "mermaid" {
		w.WriteString(`<pre class="mermaid">`)
		template.HTMLEscape(w, buf.Bytes())
		w.WriteString("</pre>\n")
	} else {
		if lang != "" {
			w.WriteString(`<pre><code class="language-`)
			w.WriteString(template.HTMLEscapeString(lang))
			w.WriteString(`">`)
		} else {
			w.WriteString("<pre><code>")
		}
		template.HTMLEscape(w, buf.Bytes())
		w.WriteString("</code></pre>\n")
	}
	return ast.WalkSkipChildren, nil
}

// Render converts markdown src to safe HTML. Mermaid fenced blocks are
// wrapped in <pre class="mermaid"> for client-side rendering by mermaid.js.
// Render converts Markdown source to safe HTML, sanitizing links and disabling raw HTML.
func Render(src string) string {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(
				util.Prioritized(&linkSanitizer{}, 999),
			),
		),
		goldmark.WithRendererOptions(
			ghtml.WithHardWraps(),
			renderer.WithNodeRenderers(
				util.Prioritized(&mermaidRenderer{}, 1),
			),
		),
	)
	if err := md.Convert([]byte(src), &buf); err != nil {
		slog.Warn("markdown: failed to convert content, falling back to escaped source", "error", err)
		return html.EscapeString(src)
	}
	return buf.String()
}
