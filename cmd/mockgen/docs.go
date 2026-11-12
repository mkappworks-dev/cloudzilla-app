package main

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	regionStart = "mockgen:start"
	regionEnd   = "mockgen:end"
)

// rewriteRegion replaces what sits between the mockgen markers in path and leaves the rest of the file alone. The
// markers are an HTML comment in index.html and in COVERAGE.md alike, which Prettier keeps as written.
func rewriteRegion(path, body string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	start := bytes.Index(src, []byte("<!-- "+regionStart+" -->"))
	if start < 0 {
		return fmt.Errorf("%s: missing <!-- %s --> / <!-- %s --> markers", path, regionStart, regionEnd)
	}
	startEnd := start + len("<!-- "+regionStart+" -->")
	end := bytes.Index(src[startEnd:], []byte("<!-- "+regionEnd+" -->"))
	if end < 0 {
		return fmt.Errorf("%s: missing <!-- %s --> / <!-- %s --> markers", path, regionStart, regionEnd)
	}
	end += startEnd
	out := string(src[:startEnd]) + "\n" + body + string(src[end:])
	return os.WriteFile(path, []byte(out), 0o644)
}

// indexRegion is the "Generated" section of index.html: one card per page, with a link per variant.
func indexRegion(pages []page) string {
	var b strings.Builder
	b.WriteString(`  <section aria-labelledby="g-generated">
    <div class="flex items-baseline justify-between mb-3">
      <h2 id="g-generated" class="text-sm font-semibold tracking-tight">Generated from the templates</h2>
      <span class="font-mono text-[11px] text-muted-foreground">` + fmt.Sprint(len(pages)) + `</span>
    </div>
    <ul class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
`)
	for _, p := range pages {
		fmt.Fprintf(&b, `      <li class="block h-full rounded-md border border-border bg-card p-4">
        <p class="font-mono text-[11px] text-muted-foreground truncate">%s</p>
        <h3 class="mt-1 text-sm font-medium">%s</h3>
        <ul class="mt-2 space-y-1">
`, html.EscapeString(p.Route), html.EscapeString(p.Title))
		for _, v := range p.Variants {
			label := "default"
			if v.Name != "" {
				label = v.Name
			}
			fmt.Fprintf(&b, `          <li><a href="generated/%s.html" class="font-mono text-[11px] text-primary hover:underline">%s</a> <span class="text-[11px] text-muted-foreground">%s</span></li>
`, fileStem(p, v), html.EscapeString(label), html.EscapeString(v.Note))
		}
		b.WriteString("        </ul>\n      </li>\n")
	}
	b.WriteString("    </ul>\n  </section>\n  ")
	return b.String()
}

// coverageRegion lists the generated variants. It is a Markdown table, written already aligned the way Prettier would
// leave it so the Format job passes.
func coverageRegion(pages []page) string {
	type row struct{ page, variant, note, templ string }
	rows := []row{}
	for _, p := range pages {
		for _, v := range p.Variants {
			label := "default"
			if v.Name != "" {
				label = v.Name
			}
			rows = append(rows, row{"`" + p.Name + "`", "`" + label + "`", v.Note, "`" + p.Templ + "`"})
		}
	}
	head := row{"Page", "Variant", "Shows", "Template"}
	widths := [4]int{utf8.RuneCountInString(head.page), utf8.RuneCountInString(head.variant), utf8.RuneCountInString(head.note), utf8.RuneCountInString(head.templ)}
	for _, r := range rows {
		widths[0] = max(widths[0], utf8.RuneCountInString(r.page))
		widths[1] = max(widths[1], utf8.RuneCountInString(r.variant))
		widths[2] = max(widths[2], utf8.RuneCountInString(r.note))
		widths[3] = max(widths[3], utf8.RuneCountInString(r.templ))
	}
	line := func(r row) string {
		return fmt.Sprintf("| %-*s | %-*s | %-*s | %-*s |\n", widths[0], r.page, widths[1], r.variant, widths[2], r.note, widths[3], r.templ)
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(line(head))
	fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", strings.Repeat("-", widths[0]), strings.Repeat("-", widths[1]),
		strings.Repeat("-", widths[2]), strings.Repeat("-", widths[3]))
	for _, r := range rows {
		b.WriteString(line(r))
	}
	b.WriteString("\n")
	return b.String()
}
