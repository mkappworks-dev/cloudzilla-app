// Package highlight renders source code as HTML token spans. Colors come from
// code-themes.css, keyed by attributes on <html>, so one rendering serves
// every user's code theme.
package highlight

import (
	"html"
	"html/template"
	"path"
	"reflect"
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
	// Some lexers (Jungle, JSONata) emit empty tokens forever on input like "{" without advancing.
	maxEmptyTokens = 1000
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
	return join(lines(blockLexer(lang, filename, src), src, now().Add(callDeadline)))
}

// BlockWithin is Block charged against b.
func BlockWithin(b *Budget, lang, filename, src string) template.HTML {
	lexer := blockLexer(lang, filename, src)
	deadline, ok := b.charge(lexer, src)
	if !ok {
		return ""
	}
	return join(lines(lexer, src, deadline))
}

func blockLexer(lang, filename, src string) chroma.Lexer {
	if lang != "" {
		return lexers.Get(lang)
	}
	return fileLexer(filename, src)
}

func join(parts []template.HTML) template.HTML {
	var b strings.Builder
	for i, l := range parts {
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
	deadline, ok := b.charge(lexer, src)
	if !ok {
		return nil
	}
	return lines(lexer, src, deadline)
}

// charge takes len(src) from b and returns the deadline for highlighting src.
// ok is false, and b is left alone, for a source that would render plain anyway
// or that b can't afford.
func (b *Budget) charge(lexer chroma.Lexer, src string) (deadline time.Time, ok bool) {
	if isPlain(lexer) || len(src) > MaxBytes || len(src) > b.bytes {
		return time.Time{}, false
	}
	start := now()
	if !start.Before(b.deadline) {
		return time.Time{}, false
	}
	b.bytes -= len(src)
	deadline = start.Add(callDeadline)
	if b.deadline.Before(deadline) {
		deadline = b.deadline
	}
	return deadline, true
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
	if _, refused := l.(refusedLexer); refused {
		return true
	}
	return l == nil || l.Config().Name == "plaintext"
}

// Delegating lexers (Svelte, ERB, PHTML, YAML+Jinja, Go HTML Template) lex
// their whole input inside Tokenise, before the first deadline check.
var delegatingLexerType = reflect.TypeOf(chroma.DelegatingLexer(nil, nil))

// Nested lexers (Markdown fences, HTTP bodies, Svelte's TypeScript) are looked
// up by name in chroma's global registry, so the stand-ins must replace them there.
func init() {
	for _, l := range slices.Clone(lexers.GlobalLexerRegistry.Lexers) {
		if reflect.TypeOf(l) == delegatingLexerType {
			lexers.Register(refusedLexer{l.Config()})
		}
	}
}

// refusedLexer stands in for a lexer that can't be bounded: it renders plain
// when picked directly and emits its input as one Text token when nested.
type refusedLexer struct{ config *chroma.Config }

func (r refusedLexer) Config() *chroma.Config { return r.config }

func (r refusedLexer) Tokenise(_ *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	return chroma.Literator(chroma.Token{Type: chroma.Text, Value: text}), nil
}

func (r refusedLexer) SetRegistry(*chroma.LexerRegistry) chroma.Lexer { return r }

func (r refusedLexer) SetAnalyser(func(string) float32) chroma.Lexer { return r }

func (refusedLexer) AnalyseText(string) float32 { return 0 }

func lines(lexer chroma.Lexer, src string, deadline time.Time) []template.HTML {
	if isPlain(lexer) || len(src) > MaxBytes {
		return nil
	}
	// EnsureLF off: it rewrites \r\n, and every line's text must stay equal to the source.
	// No chroma.Coalesce: it loops inside one it() call, past the deadline check.
	it, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root"}, src)
	if err != nil {
		return nil
	}
	var out []template.HTML
	var texts []string
	var line lineWriter
	var text strings.Builder
	empty := 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		if now().After(deadline) {
			return nil
		}
		if tok.Value == "" {
			if empty++; empty > maxEmptyTokens {
				return nil
			}
			continue
		}
		empty = 0
		class := tokenClass(tok.Type)
		v := tok.Value
		for {
			part, rest, more := strings.Cut(v, "\n")
			line.write(class, part)
			text.WriteString(part)
			if !more {
				break
			}
			out = append(out, line.finish())
			texts = append(texts, text.String())
			text.Reset()
			v = rest
		}
	}
	out = append(out, line.finish())
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

// lineWriter builds one line's HTML, extending the open span while the class repeats.
type lineWriter struct {
	b    strings.Builder
	open string
}

func (w *lineWriter) write(class, text string) {
	if text == "" {
		return
	}
	if class != w.open {
		if w.open != "" {
			w.b.WriteString(`</span>`)
		}
		if class != "" {
			w.b.WriteString(`<span class="`)
			w.b.WriteString(class)
			w.b.WriteString(`">`)
		}
		w.open = class
	}
	w.b.WriteString(html.EscapeString(text))
}

func (w *lineWriter) finish() template.HTML {
	if w.open != "" {
		w.b.WriteString(`</span>`)
	}
	h := template.HTML(w.b.String())
	w.b.Reset()
	w.open = ""
	return h
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
