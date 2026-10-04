// Package highlight renders source code as HTML token spans. Colors come from
// code-themes.css, keyed by attributes on <html>, so one rendering serves
// every user's code theme.
package highlight

import (
	"html"
	"html/template"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// MaxBytes is the largest source that gets highlighted; bigger ones render plain.
const MaxBytes = 512 << 10

const (
	// chroma's per-match regexp timeout doesn't bound a whole file, so each call has its own.
	callDeadline = 500 * time.Millisecond
	sniffBytes   = 1 << 10
)

var now = time.Now

// Lines returns one HTML fragment per "\n"-separated line of src, or nil when
// src should render as plain text. Each fragment's text is exactly its line.
func Lines(filename, src string) []template.HTML {
	return lines(fileLexer(filename, src), src, now().Add(callDeadline))
}

// Block returns src highlighted for a <pre>, or "" for plain text. lang is a
// Markdown info string such as "go"; filename picks the lexer when lang is "".
func Block(lang, filename, src string) template.HTML {
	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	} else {
		lexer = fileLexer(filename, src)
	}
	out := lines(lexer, src, now().Add(callDeadline))
	if out == nil {
		return ""
	}
	var b strings.Builder
	for i, l := range out {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	return template.HTML(b.String())
}

// Budget caps the highlighting for one page of many files; files past it render plain.
type Budget struct {
	bytes    int
	deadline time.Time
}

func NewBudget(maxBytes int, maxTime time.Duration) *Budget {
	return &Budget{bytes: maxBytes, deadline: now().Add(maxTime)}
}

// LinesWithin is Lines charged against b.
func LinesWithin(b *Budget, filename, src string) []template.HTML {
	lexer := fileLexer(filename, src)
	if isPlain(lexer) || len(src) > b.bytes {
		return nil
	}
	b.bytes -= len(src)
	deadline := now().Add(callDeadline)
	if b.deadline.Before(deadline) {
		deadline = b.deadline
	}
	return lines(lexer, src, deadline)
}

// fileLexer picks a lexer by file name. Only shebang scripts are sniffed;
// other extensionless files (LICENSE, README) stay plain.
func fileLexer(filename, src string) chroma.Lexer {
	base := path.Base(filename)
	if l := lexers.Match(base); l != nil {
		return l
	}
	if path.Ext(base) == "" && strings.HasPrefix(src, "#!") {
		head := src
		if len(head) > sniffBytes {
			head = head[:sniffBytes]
		}
		return lexers.Analyse(head)
	}
	return nil
}

func isPlain(l chroma.Lexer) bool {
	return l == nil || l.Config().Name == "plaintext"
}

func lines(lexer chroma.Lexer, src string, deadline time.Time) []template.HTML {
	if isPlain(lexer) || len(src) > MaxBytes {
		return nil
	}
	// EnsureLF off: it rewrites \r\n, and every line's text must stay equal to the source.
	it, err := chroma.Coalesce(lexer).Tokenise(&chroma.TokeniseOptions{State: "root"}, src)
	if err != nil {
		return nil
	}
	var out []template.HTML
	var texts []string
	var line, text strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		if now().After(deadline) {
			return nil
		}
		class := tokenClass(tok.Type)
		v := tok.Value
		for {
			part, rest, more := strings.Cut(v, "\n")
			writeToken(&line, class, part)
			text.WriteString(part)
			if !more {
				break
			}
			out = append(out, template.HTML(line.String()))
			texts = append(texts, text.String())
			line.Reset()
			text.Reset()
			v = rest
		}
	}
	out = append(out, template.HTML(line.String()))
	texts = append(texts, text.String())

	want := strings.Split(src, "\n")
	// Lexers configured with EnsureNL append a newline src may lack.
	if len(out) == len(want)+1 && texts[len(texts)-1] == "" {
		out, texts = out[:len(want)], texts[:len(want)]
	}
	if !slices.Equal(texts, want) {
		return nil
	}
	return out
}

func writeToken(b *strings.Builder, class, text string) {
	if text == "" {
		return
	}
	if class == "" {
		b.WriteString(html.EscapeString(text))
		return
	}
	b.WriteString(`<span class="`)
	b.WriteString(class)
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(text))
	b.WriteString(`</span>`)
}

// tokenClass returns the class code-themes.css colors tt by, or "" for the
// theme's base color. Error tokens stay plain: a diff hunk or an unclosed
// fence trips them constantly, and some themes paint them red.
func tokenClass(tt chroma.TokenType) string {
	for t := tt; t > 0; t = t.Parent() {
		if short, ok := chroma.StandardTypes[t]; ok {
			if short == "" || t == chroma.Whitespace {
				return ""
			}
			return "hl-" + short
		}
	}
	return ""
}
