package main

import (
	"bytes"
	"context"
	"regexp"
)

var (
	scriptTag     = regexp.MustCompile(`(?s)<script\b[^>]*>.*?</script>\s*`)
	themeBoot     = regexp.MustCompile(`(?s)<script>\s*// Theme bootstrap.*?</script>`)
	mainCSS       = regexp.MustCompile(`<link rel="stylesheet" href="/static/main\.css"\s*/?>`)
	codeThemesCSS = regexp.MustCompile(`\s*<link rel="stylesheet" href="/static/code-themes\.css"\s*/?>`)
	favicon       = regexp.MustCompile(`href="/static/favicon\.svg"`)
)

// renderVariant renders the real component, then makes it a static mock: server.js builds the stylesheet, and the
// page's scripts are replaced by mock-ui.js, so a mock carries no htmx, Alpine or request handling.
func renderVariant(ctx context.Context, p page, v variant) ([]byte, error) {
	var buf bytes.Buffer
	if err := v.Render(baseFor(v)).Render(ctx, &buf); err != nil {
		return nil, err
	}
	return staticize(buf.Bytes()), nil
}

func staticize(html []byte) []byte {
	boot := themeBoot.Find(html)
	html = scriptTag.ReplaceAll(html, nil)
	html = mainCSS.ReplaceAll(html, []byte(`<link rel="stylesheet" href="/assets/tailwind.css"/>`))
	html = codeThemesCSS.ReplaceAll(html, nil)
	html = favicon.ReplaceAll(html, []byte(`href="/favicon.svg"`))
	// The bootstrap sets the theme before paint; every other script is gone, so put it back at the end of <head>.
	html = bytes.Replace(html, []byte("</head>"), append(append([]byte("\t"), boot...), []byte("\n\t</head>")...), 1)
	return bytes.Replace(html, []byte("</body>"), []byte("\t<script src=\"/mock-ui.js\"></script>\n\t</body>"), 1)
}
