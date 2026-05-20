package handler

import (
	"path/filepath"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// renderMentionsHTML wraps @username tokens in the already-HTML-rendered string
// with profile anchor tags. Input is trusted HTML from the markdown renderer.
func renderMentionsHTML(html string) string {
	return service.MentionRegex.ReplaceAllStringFunc(html, func(match string) string {
		user := match[1:] // strip leading @
		return `<a href="/` + user + `" class="text-blue-600 hover:underline">` + match + `</a>`
	})
}

// gistLanguage derives a display label and Tailwind chip class from a slice of
// filenames. The first filename's extension drives the result. Returns ("", "")
// when no recognisable extension is found.
func gistLanguage(filenames []string) (label, chipClass string) {
	if len(filenames) == 0 {
		return "", ""
	}
	ext := strings.ToLower(filepath.Ext(filenames[0]))
	switch ext {
	case ".css":
		return "CSS", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-primary/15 text-primary border-primary/30 whitespace-nowrap"
	case ".sh", ".bash":
		return "Bash", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-success/15 text-success border-success/30 whitespace-nowrap"
	case ".sql":
		return "SQL", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-warning/15 text-warning border-warning/30 whitespace-nowrap"
	case ".py":
		return "Python", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-[hsl(270_70%_50%/0.15)] text-[hsl(270_70%_70%)] border-[hsl(270_70%_50%/0.30)] whitespace-nowrap"
	case ".js":
		return "JavaScript", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-warning/15 text-warning border-warning/30 whitespace-nowrap"
	case ".ts":
		return "TypeScript", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-primary/15 text-primary border-primary/30 whitespace-nowrap"
	case ".go":
		return "Go", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-primary/15 text-primary border-primary/30 whitespace-nowrap"
	case ".md":
		return "Markdown", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border border-border bg-accent text-muted-foreground whitespace-nowrap"
	case ".rb":
		return "Ruby", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-[hsl(270_70%_50%/0.15)] text-[hsl(270_70%_70%)] border-[hsl(270_70%_50%/0.30)] whitespace-nowrap"
	case ".rs":
		return "Rust", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-warning/15 text-warning border-warning/30 whitespace-nowrap"
	case ".json":
		return "JSON", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border border-border bg-accent text-muted-foreground whitespace-nowrap"
	case ".yaml", ".yml":
		return "YAML", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border border-border bg-accent text-muted-foreground whitespace-nowrap"
	case ".html", ".htm":
		return "HTML", "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-warning/15 text-warning border-warning/30 whitespace-nowrap"
	default:
		return "", ""
	}
}
