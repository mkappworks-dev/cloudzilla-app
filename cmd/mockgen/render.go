package main

import (
	"bytes"
	"context"
	"errors"
	"regexp"
)

var (
	scriptTag     = regexp.MustCompile(`(?s)<script\b[^>]*>.*?</script>\s*`)
	themeBoot     = regexp.MustCompile(`(?s)<script>\s*// Theme bootstrap.*?</script>`)
	mainCSS       = regexp.MustCompile(`<link rel="stylesheet" href="/static/main\.css"\s*/?>`)
	codeThemesCSS = regexp.MustCompile(`\s*<link rel="stylesheet" href="/static/code-themes\.css"\s*/?>`)
	favicon       = regexp.MustCompile(`href="/static/favicon\.svg"`)
	// The view helpers count whole years off time.Now ("1y", "1y ago") for anything older than a year, which would
	// change the committed output every year and fail the CI freshness check.
	yearsAgo = regexp.MustCompile(`([\s>])\d+y( ago)?<`)
)

// renderVariant renders the real component, then makes it a static mock: server.js builds the stylesheet, and the
// page's scripts are replaced by mock-ui.js, so a mock carries no htmx, Alpine or request handling.
func renderVariant(ctx context.Context, v variant) ([]byte, error) {
	var buf bytes.Buffer
	if err := v.Render(baseFor(v)).Render(ctx, &buf); err != nil {
		return nil, err
	}
	return staticize(buf.Bytes())
}

func staticize(html []byte) ([]byte, error) {
	boot := themeBoot.Find(html)
	if boot == nil {
		return nil, errors.New("theme bootstrap script not found in layout.Base; update themeBoot in render.go")
	}
	html = scriptTag.ReplaceAll(html, nil)
	html = mainCSS.ReplaceAll(html, []byte(`<link rel="stylesheet" href="/assets/tailwind.css"/>`))
	html = codeThemesCSS.ReplaceAll(html, nil)
	html = yearsAgo.ReplaceAll(html, []byte("${1}1y${2}<"))
	html = favicon.ReplaceAll(html, []byte(`href="/favicon.svg"`))
	// The bootstrap sets the theme before paint; every other script is gone, so put it back at the end of <head>.
	html = bytes.Replace(html, []byte("</head>"), append(append([]byte("\t"), boot...), []byte("\n\t</head>")...), 1)
	return bytes.Replace(html, []byte("</body>"), []byte("\t<script src=\"/mock-ui.js\"></script>\n\t</body>"), 1), nil
}
