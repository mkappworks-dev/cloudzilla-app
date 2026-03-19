package markdown

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

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
func Render(src string) template.HTML {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(
			ghtml.WithHardWraps(),
			renderer.WithNodeRenderers(
				util.Prioritized(&mermaidRenderer{}, 1),
			),
		),
	)
	if err := md.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}
