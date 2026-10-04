package highlight

import (
	"fmt"
	"html/template"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// returnsWithin runs f on its own goroutine so a hung lexer fails the test
// instead of hanging it; the goroutine leaks on failure.
func returnsWithin(d time.Duration, f func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// repeatTo repeats s whole, so a closing fence after it starts its own line.
func repeatTo(s string, n int) string {
	return strings.Repeat(s, n/len(s))
}

const mixedSnippet = `#!/usr/bin/env x
<script>let x = {a: "b", c: ` + "`t${1}`" + `};</script>
<style>.a { color: red; } @media (x) { b { c: d } }</style>
{#if x}<p class="a" on:click={() => x++}>{x}</p>{/if}
<?php echo "x"; ?> <%= y %> {{ .Z }} {% if a %}{{ b }}{% endif %}
/* c */ // d "e\"f" 'g' [[h]] (( i )) # j ; k -- l %% m
- key: value
  list: [1, 2.5e3, 0x1F, -7]
SELECT * FROM t WHERE a = 'b';
def f(x): return x ** 2 \begin{x} \end{x} <!-- n --> <![CDATA[ o ]]>
λ ünïcödé → ✓ é \x41 $var @attr &amp;
`

// Delegating lexers lex their whole input inside Tokenise, before the first
// deadline check; braces and tags are what make Svelte's slow.
const fenceSnippet = "{x} {y + 1} <p>{z}</p>\n"

// The Markdown lexer only accepts \w+ fence languages, so Go HTML Template
// can't be reached through a fence.
var delegatingFenceLangs = []string{"svelte", "erb", "phtml", "salt"}

func adversarialCorpus() map[string]string {
	corpus := map[string]string{}
	for _, s := range []string{"{", `"`, "<", "#", "`", "/*"} {
		corpus[fmt.Sprintf("%q", s)] = s
		corpus[fmt.Sprintf("%q", s+"\n")] = s + "\n"
	}
	corpus["mixed ~48 KiB"] = repeatTo(mixedSnippet, 48<<10) + "\n/* \"unterminated <!-- `"
	corpus["braces and tags ~64 KiB"] = repeatTo(fenceSnippet, 64<<10)
	return corpus
}

func TestLexers_EveryRegisteredLexerReturnsPromptly(t *testing.T) {
	const timeout = 1500 * time.Millisecond
	corpus := adversarialCorpus()
	for _, l := range lexers.GlobalLexerRegistry.Lexers {
		name := l.Config().Name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for input, src := range corpus {
				if !returnsWithin(timeout, func() { lines(l, src, time.Now().Add(callDeadline)) }) {
					t.Errorf("lexer %q on %s did not return within %v", name, input, timeout)
					return
				}
			}
		})
	}
	md := lexers.Get("markdown")
	for _, lang := range delegatingFenceLangs {
		t.Run("markdown fence "+lang, func(t *testing.T) {
			t.Parallel()
			src := "# Doc\n\n```" + lang + "\n" + repeatTo(fenceSnippet, 64<<10) + "```\n"
			if !returnsWithin(timeout, func() { lines(md, src, time.Now().Add(callDeadline)) }) {
				t.Errorf("markdown with a 64 KiB %s fence did not return within %v", lang, timeout)
			}
		})
	}
}

func TestLexers_RegistryHoldsNoDelegatingLexer(t *testing.T) {
	delegating := reflect.TypeOf(chroma.DelegatingLexer(nil, nil))
	for _, l := range lexers.GlobalLexerRegistry.Lexers {
		if reflect.TypeOf(l) == delegating {
			t.Errorf("lexer %q is a delegating lexer, which lexes its whole input before returning", l.Config().Name)
		}
	}
}

func TestLines_DelegatingLanguagesRenderPlain(t *testing.T) {
	for _, filename := range []string{"App.svelte", "index.html.erb", "page.phtml", "top.sls"} {
		if got := Lines(filename, "<p>{x}</p>\n"); got != nil {
			t.Errorf("Lines(%q) = %q, want plain", filename, got)
		}
	}
}

// It also checks that the sweep's fence documents reach the nested lookup.
func TestLines_FenceInADelegatingLanguageRendersPlain(t *testing.T) {
	for _, lang := range delegatingFenceLangs {
		got := Lines("README.md", "# Doc\n\n```"+lang+"\n"+fenceSnippet+"```\n")
		if got == nil {
			t.Fatalf("Lines(README.md with a %s fence) = nil, want the Markdown highlighted", lang)
		}
		if want := template.HTML(`<span class="hl-s">` + "```" + lang + `</span>`); got[2] != want {
			t.Errorf("fence opener = %q, want %q: markdown didn't lex it as a fence", got[2], want)
		}
		if strings.Contains(string(got[3]), "<span") {
			t.Errorf("%s fence line = %q, want no spans: nested lookups must not reach a delegating lexer", lang, got[3])
		}
	}
}
