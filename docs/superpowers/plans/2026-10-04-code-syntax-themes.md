# Code Syntax Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Syntax-highlight code across the app (files, blame, gists, diffs, Markdown fences) and let each user pick a code theme per site mode in Settings.

**Architecture:** A pure `internal/highlight` package runs Chroma on the server and emits `<span class="hl-k">` token spans with no inline colors. A generated, committed stylesheet (`code-themes.css`) holds every curated theme, scoped by `html.dark[data-code-dark="…"]` / `html:not(.dark)[data-code-light="…"]`. The layout writes the user's two saved theme IDs onto `<html>`, so the existing light/dark toggle switches code themes with pure CSS.

**Tech Stack:** Go 1.27, `github.com/alecthomas/chroma/v2` v2.27.0, goldmark v2, Templ, HTMX, PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-10-04-code-syntax-themes-design.md`

## Global Constraints

- Layering is strict: stores hold SQL, services hold logic, handlers call services. `internal/highlight` is a leaf package (like `internal/markdown`) that services, views and markdown may import.
- `context.Context` is the first argument of every store and service method.
- Edit `.templ` files only, then run `make generate-templ` **in its own Bash call** and grep the `_templ.go` for a string you added (the "updates=N" count is unreliable). Never hand-edit `_templ.go`. Never run `templ fmt`.
- Comments: only for a *why* the code can't show, one line by default. No task/phase narration ("Task 3:", "added for …", "used by …"). Match the surrounding comment density.
- Keep any `{{if}}` / `templ.KV` expression inside a `class` attribute on one line.
- `templ.Raw` is used only for `internal/highlight` output, which escapes every token value.
- Curated themes, exact chroma IDs. Light: `github`, `solarized-light`, `catppuccin-latte`, `gruvbox-light`, `tokyonight-day`, `rose-pine-dawn`. Dark: `github-dark`, `onedark`, `dracula`, `monokai`, `nord`, `solarized-dark`, `catppuccin-mocha`, `tokyonight-night`, `gruvbox`. Defaults: `github` / `github-dark`.
- Caps: 512 KiB per highlighted source; 500 ms per highlight call; 4 MiB and 2 s per diff page.
- `git add` specific paths only; never stage `.claude/`. Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Integration tests need `TEST_DATABASE_DSN` (the controller supplies it); without it they skip, and a skip is not a pass for this plan's DB tasks.
- Run Go with plain `go` (the Makefile's `GO` var points at `/usr/local/go/bin/go`).

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/highlight/highlight.go` (new) | `Lines`, `Block`, `Budget`, `LinesWithin`, lexer choice, token → class, guards |
| `internal/highlight/themes.go` (new) | Theme catalog, defaults, `Normalize*`, `LightThemes`/`DarkThemes` |
| `internal/highlight/stylesheet.go` (new) | `Stylesheet()` CSS generator |
| `cmd/gen-code-themes/main.go` (new) | Writes `Stylesheet()` to `cmd/server/frontend/static/code-themes.css` |
| `cmd/server/frontend/static/code-themes.css` (new, generated, committed) | All theme CSS |
| `internal/db/migrations/101_user_code_themes.sql` (new) | Two `users` columns |
| `internal/model/user.go`, `internal/store/user_store.go`, `internal/service/user_service.go` | Persist and read code themes |
| `internal/view/viewmodels.go`, `internal/handler/page_handler.go`, `internal/view/layout/layout.templ` | Put themes on `<html>`, link the stylesheet |
| `internal/handler/settings_handler.go`, `internal/router/router.go`, `internal/view/pages/settings.templ`, `internal/view/pages/settings_helpers.go` | Appearance settings |
| `internal/view/components/code_text.templ` (new) | `CodeText`, `DiffCode` |
| `internal/service/code_service.go`, `code_service_tree.go`, `code_service_blame.go` | Highlight blob and blame lines |
| `internal/service/code_service_commit.go`, `code_service_merge.go`, `code_service_highlight.go` (new) | Blob hashes on `FileDiff`, `DiffLine.HTML`, `HighlightDiffs` |
| `internal/markdown/markdown.go` | Highlight fenced blocks |
| `docs/code-browser.md` | Document highlighting |

---

### Task 1: `internal/highlight` core

**Files:**
- Create: `internal/highlight/highlight.go`
- Test: `internal/highlight/highlight_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `const MaxBytes = 512 << 10`
  - `func Lines(filename, src string) []template.HTML`
  - `func Block(lang, filename, src string) template.HTML`
  - `type Budget struct{…}`; `func NewBudget(maxBytes int, maxTime time.Duration) *Budget`
  - `func LinesWithin(b *Budget, filename, src string) []template.HTML`
  - unexported `tokenClass(chroma.TokenType) string` (Task 2 relies on the `"hl-" + StandardTypes short name` scheme)

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/alecthomas/chroma/v2@v2.27.0`
Expected: `go.mod` gains `github.com/alecthomas/chroma/v2 v2.27.0` (and `github.com/dlclark/regexp2` indirect). The module is already in the local cache.

- [ ] **Step 2: Write the failing tests**

Create `internal/highlight/highlight_test.go`. These tests are not `t.Parallel()`: the deadline test swaps the package `now` var.

```go
package highlight

import (
	"html"
	"html/template"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
)

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func plainText(h template.HTML) string {
	return html.UnescapeString(tagPattern.ReplaceAllString(string(h), ""))
}

func TestLines_TextMatchesEachSourceLine(t *testing.T) {
	for _, tc := range []struct{ name, filename, src string }{
		{"go with trailing newline", "main.go", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"},
		{"go without trailing newline", "main.go", "package main\n\nvar x = 1"},
		{"crlf python", "app.py", "def f():\r\n    return 1\r\n"},
		{"multi-line comment", "main.c", "/* one\n * two\n */\nint x;\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Lines(tc.filename, tc.src)
			want := strings.Split(tc.src, "\n")
			if len(got) != len(want) {
				t.Fatalf("len = %d, want %d", len(got), len(want))
			}
			for i := range want {
				if p := plainText(got[i]); p != want[i] {
					t.Errorf("line %d text = %q, want %q", i+1, p, want[i])
				}
			}
		})
	}
}

func TestLines_WrapsTokensInClasses(t *testing.T) {
	got := Lines("main.go", "package main\n\nfunc main() {}\n")
	if !strings.HasPrefix(string(got[0]), `<span class="hl-k`) || !strings.Contains(string(got[0]), `>package</span>`) {
		t.Errorf("line 1 = %q, want a keyword span around package", got[0])
	}
}

func TestLines_CommentContinuesAcrossLines(t *testing.T) {
	got := Lines("main.c", "/* one\n * two\n */\nint x;\n")
	for i := 0; i < 3; i++ {
		if !strings.Contains(string(got[i]), `class="hl-cm"`) {
			t.Errorf("line %d = %q, want a multi-line comment span", i+1, got[i])
		}
	}
}

func TestLines_EscapesHTML(t *testing.T) {
	got := Lines("main.go", "package main\n\nvar s = \"<script>alert(1)</script>\"\n")
	if strings.Contains(string(got[2]), "<script>") {
		t.Errorf("line 3 = %q, want <script> escaped", got[2])
	}
	if !strings.Contains(string(got[2]), "&lt;script&gt;") {
		t.Errorf("line 3 = %q, want &lt;script&gt;", got[2])
	}
}

func TestLines_PlainWhenThereIsNoUsefulLexer(t *testing.T) {
	for _, filename := range []string{"notes.txt", "data.nosuchext", "LICENSE", ""} {
		if got := Lines(filename, "some words here\n"); got != nil {
			t.Errorf("Lines(%q) = %q, want nil", filename, got)
		}
	}
}

func TestLines_SniffsShebangScripts(t *testing.T) {
	if got := Lines("deploy", "#!/usr/bin/env python\nprint(1)\n"); got == nil {
		t.Error("Lines(shebang script) = nil, want highlighted")
	}
}

func TestLines_PlainOverMaxBytes(t *testing.T) {
	src := "package main\n" + strings.Repeat("// x\n", MaxBytes/5)
	if got := Lines("main.go", src); got != nil {
		t.Errorf("Lines(%d bytes) highlighted, want nil", len(src))
	}
}

func TestLines_GivesUpPastTheDeadline(t *testing.T) {
	start := time.Now()
	calls := 0
	now = func() time.Time {
		calls++
		return start.Add(time.Duration(calls) * time.Second)
	}
	t.Cleanup(func() { now = time.Now })
	if got := Lines("main.go", "package main\n"); got != nil {
		t.Errorf("Lines = %q, want nil once the deadline passes", got)
	}
}

func TestLinesWithin_StopsAtTheByteBudget(t *testing.T) {
	src := "package main\n\nvar x = 1\n" // 24 bytes
	b := NewBudget(40, time.Minute)
	if got := LinesWithin(b, "a.go", src); got == nil {
		t.Fatal("first file = nil, want highlighted")
	}
	if got := LinesWithin(b, "b.go", src); got != nil {
		t.Error("second file highlighted past the budget, want nil")
	}
}

func TestLinesWithin_DoesNotChargePlainFiles(t *testing.T) {
	b := NewBudget(30, time.Minute)
	if got := LinesWithin(b, "notes.txt", strings.Repeat("x", 20)); got != nil {
		t.Fatalf("plain file highlighted: %q", got)
	}
	if got := LinesWithin(b, "a.go", "package main\n\nvar x = 1\n"); got == nil {
		t.Error("Go file = nil, want highlighted: the plain file should not use the budget")
	}
}

func TestBlock(t *testing.T) {
	if got := Block("go", "", "x := 1\ny := 2\n"); !strings.Contains(string(got), "<span") || !strings.HasSuffix(string(got), "\n") {
		t.Errorf(`Block("go") = %q, want spans and the trailing newline kept`, got)
	}
	if got := Block("", "main.py", "print(1)\n"); got == "" {
		t.Error(`Block("", "main.py") = "", want highlighted by filename`)
	}
	for _, lang := range []string{"nosuchlang", "text", `"><script>alert(1)</script>`} {
		if got := Block(lang, "", "x\n"); got != "" {
			t.Errorf("Block(%q) = %q, want plain", lang, got)
		}
	}
	if got := Block("go", "", ""); got != "" {
		t.Errorf(`Block("go", empty) = %q, want ""`, got)
	}
}

func TestTokenClass(t *testing.T) {
	for _, tc := range []struct {
		tt   chroma.TokenType
		want string
	}{
		{chroma.Keyword, "hl-k"},
		{chroma.CommentMultiline, "hl-cm"},
		{chroma.Text, ""},
		{chroma.Whitespace, ""},
		{chroma.Error, ""},
	} {
		if got := tokenClass(tc.tt); got != tc.want {
			t.Errorf("tokenClass(%v) = %q, want %q", tc.tt, got, tc.want)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/highlight/`
Expected: FAIL to compile, with `undefined: Lines`, `undefined: MaxBytes` and so on.

- [ ] **Step 4: Implement `internal/highlight/highlight.go`**

```go
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/highlight/ -v`
Expected: PASS. If a lexer detail differs from an assertion (e.g. the C lexer's comment class), confirm what chroma emits with a one-off `fmt.Println(Lines(...))` in a test before changing either side. The invariants that must hold are the text-equality check, escaping, and the nil cases.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/highlight/highlight.go internal/highlight/highlight_test.go
git commit -m "feat(highlight): tokenise source into themed HTML spans with Chroma"
```

---

### Task 2: Theme catalog and generated stylesheet

**Files:**
- Create: `internal/highlight/themes.go`, `internal/highlight/stylesheet.go`, `cmd/gen-code-themes/main.go`, `cmd/server/frontend/static/code-themes.css` (generated)
- Test: `internal/highlight/themes_test.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes: the `hl-<short>` class scheme from Task 1's `tokenClass`.
- Produces:
  - `type Theme struct{ ID, Name string; Dark bool }`; `var Themes []Theme`
  - `const DefaultLight = "github"`, `DefaultDark = "github-dark"`
  - `func LightThemes() []Theme`, `func DarkThemes() []Theme`
  - `func NormalizeLight(id string) string`, `func NormalizeDark(id string) string`
  - `func Stylesheet() []byte`
  - Makefile target `generate-code-themes`

- [ ] **Step 1: Write the failing tests**

Create `internal/highlight/themes_test.go`:

```go
package highlight

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

func TestThemes_ExistInChroma(t *testing.T) {
	for _, th := range Themes {
		// styles.Get silently falls back to another style, so check the registry.
		if styles.Registry[th.ID] == nil {
			t.Errorf("theme %q is not a chroma style", th.ID)
		}
	}
}

func TestThemes_DarkFlagMatchesBackground(t *testing.T) {
	for _, th := range Themes {
		bg := styles.Registry[th.ID].Get(chroma.Background).Background
		if !bg.IsSet() {
			t.Errorf("theme %q has no background colour", th.ID)
			continue
		}
		if dark := bg.Brightness() < 0.5; dark != th.Dark {
			t.Errorf("theme %q: Dark = %v, but background %s says %v", th.ID, th.Dark, bg, dark)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{NormalizeLight("solarized-light"), "solarized-light"},
		{NormalizeLight("dracula"), DefaultLight},
		{NormalizeLight(""), DefaultLight},
		{NormalizeDark("nord"), "nord"},
		{NormalizeDark("github"), DefaultDark},
		{NormalizeDark("retired-theme"), DefaultDark},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestLightAndDarkThemesPartitionTheCatalog(t *testing.T) {
	if len(LightThemes())+len(DarkThemes()) != len(Themes) {
		t.Fatalf("light %d + dark %d != %d themes", len(LightThemes()), len(DarkThemes()), len(Themes))
	}
	for _, th := range LightThemes() {
		if th.Dark {
			t.Errorf("LightThemes includes dark theme %q", th.ID)
		}
	}
}

func TestStylesheet_ScopesEachThemeToItsMode(t *testing.T) {
	css := string(Stylesheet())
	for _, want := range []string{
		`html:not(.dark)[data-code-light="github"] .hl {`,
		`html:not(.dark)[data-code-light="github"] .hl-k {`,
		`html.dark[data-code-dark="dracula"] .hl {`,
		`html.dark[data-code-dark="dracula"] .hl-cm {`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet lacks %q", want)
		}
	}
	if strings.Contains(css, `.hl-w {`) || strings.Contains(css, `.hl-err {`) {
		t.Error("stylesheet styles whitespace or error tokens, which Lines never emits")
	}
}

func TestStylesheet_MatchesCommittedFile(t *testing.T) {
	committed, err := os.ReadFile("../../cmd/server/frontend/static/code-themes.css")
	if err != nil {
		t.Fatalf("read code-themes.css: %v", err)
	}
	if !bytes.Equal(committed, Stylesheet()) {
		t.Error("code-themes.css is stale; run make generate-code-themes")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/highlight/`
Expected: FAIL to compile, with `undefined: Themes` and so on.

- [ ] **Step 3: Implement `internal/highlight/themes.go`**

```go
package highlight

// Theme is a code theme users can pick; ID is its chroma style name.
type Theme struct {
	ID   string
	Name string
	Dark bool
}

var Themes = []Theme{
	{"github", "GitHub", false},
	{"solarized-light", "Solarized Light", false},
	{"catppuccin-latte", "Catppuccin Latte", false},
	{"gruvbox-light", "Gruvbox Light", false},
	{"tokyonight-day", "Tokyo Night Day", false},
	{"rose-pine-dawn", "Rosé Pine Dawn", false},
	{"github-dark", "GitHub Dark", true},
	{"onedark", "One Dark", true},
	{"dracula", "Dracula", true},
	{"monokai", "Monokai", true},
	{"nord", "Nord", true},
	{"solarized-dark", "Solarized Dark", true},
	{"catppuccin-mocha", "Catppuccin Mocha", true},
	{"tokyonight-night", "Tokyo Night", true},
	{"gruvbox", "Gruvbox", true},
}

const (
	DefaultLight = "github"
	DefaultDark  = "github-dark"
)

func LightThemes() []Theme { return themesFor(false) }
func DarkThemes() []Theme  { return themesFor(true) }

func themesFor(dark bool) []Theme {
	var out []Theme
	for _, t := range Themes {
		if t.Dark == dark {
			out = append(out, t)
		}
	}
	return out
}

// NormalizeLight returns id when it is a light theme in the catalog, else the
// default, so a theme dropped from the catalog never needs a migration.
func NormalizeLight(id string) string { return normalize(id, false, DefaultLight) }

func NormalizeDark(id string) string { return normalize(id, true, DefaultDark) }

func normalize(id string, dark bool, fallback string) string {
	for _, t := range Themes {
		if t.ID == id && t.Dark == dark {
			return id
		}
	}
	return fallback
}
```

- [ ] **Step 4: Implement `internal/highlight/stylesheet.go`**

```go
package highlight

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

// Stylesheet returns the CSS for every catalog theme. Each rule is scoped by
// the site mode class and the matching data-code-* attribute on <html>, so
// toggling the site theme switches code themes without any script.
func Stylesheet() []byte {
	var b bytes.Buffer
	b.WriteString("/* Generated by make generate-code-themes from internal/highlight. */\n")
	types := tokenTypes()
	for _, th := range Themes {
		style := styles.Registry[th.ID]
		scope := `html:not(.dark)[data-code-light="` + th.ID + `"]`
		if th.Dark {
			scope = `html.dark[data-code-dark="` + th.ID + `"]`
		}
		bg := style.Get(chroma.Background)
		fmt.Fprintf(&b, "%s .hl { %s }\n", scope, chromahtml.StyleEntryToCSS(bg))
		for _, tt := range types {
			css := chromahtml.StyleEntryToCSS(style.Get(tt).Sub(bg))
			if css == "" {
				continue
			}
			fmt.Fprintf(&b, "%s .hl-%s { %s }\n", scope, chroma.StandardTypes[tt], css)
		}
	}
	return b.Bytes()
}

// tokenTypes lists, in a fixed order, the token types tokenClass can emit.
func tokenTypes() []chroma.TokenType {
	var out []chroma.TokenType
	for tt, short := range chroma.StandardTypes {
		if tt > 0 && short != "" && tt != chroma.Whitespace {
			out = append(out, tt)
		}
	}
	slices.Sort(out)
	return out
}
```

- [ ] **Step 5: Implement `cmd/gen-code-themes/main.go`**

```go
// Command gen-code-themes writes the code theme stylesheet the server embeds.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
)

func main() {
	out := flag.String("o", "cmd/server/frontend/static/code-themes.css", "output path")
	flag.Parse()
	if err := os.WriteFile(*out, highlight.Stylesheet(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-code-themes:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Add the Makefile target**

In `Makefile`, add `generate-code-themes` to the `.PHONY` line, and add this target right after the `generate-templ` target:

```make
generate-code-themes:              ## Regenerate static/code-themes.css from internal/highlight
	$(GO) run ./cmd/gen-code-themes
```

- [ ] **Step 7: Generate the stylesheet**

Run: `go run ./cmd/gen-code-themes`
Then run `head -5 cmd/server/frontend/static/code-themes.css` and `wc -c cmd/server/frontend/static/code-themes.css`.
Expected: the header comment, then `html:not(.dark)[data-code-light="github"] .hl { color: #…; background-color: #ffffff }`. The size is in the tens of KB.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/highlight/ -v`
Expected: PASS, including `TestStylesheet_MatchesCommittedFile`. If `TestThemes_DarkFlagMatchesBackground` fails for a theme, report it to the controller rather than flipping the flag: the catalog is a user decision.

- [ ] **Step 9: Commit**

```bash
git add internal/highlight/themes.go internal/highlight/stylesheet.go internal/highlight/themes_test.go cmd/gen-code-themes/main.go cmd/server/frontend/static/code-themes.css Makefile
git commit -m "feat(highlight): curated code themes and their generated stylesheet"
```

---

### Task 3: Persist each user's code themes

**Files:**
- Create: `internal/db/migrations/101_user_code_themes.sql` (first check `ls internal/db/migrations | tail -2`; if `101_` is taken, use the next number)
- Modify: `internal/model/user.go`, `internal/store/user_store.go`, `internal/service/user_service.go`
- Test: `internal/service/user_service_test.go` (append)

**Interfaces:**
- Consumes: `highlight.NormalizeLight`, `highlight.NormalizeDark`, `highlight.DefaultLight`, `highlight.DefaultDark`.
- Produces:
  - `model.User.CodeThemeLight`, `model.User.CodeThemeDark string`
  - `(*store.UserStore).UpdateCodeThemes(ctx, userID int64, light, dark string) error`
  - `(*store.UserStore).GetCodeThemes(ctx, userID int64) (light, dark string, err error)`
  - `(*service.UserService).UpdateCodeThemes(ctx, userID int64, light, dark string) error`
  - `(*service.UserService).CodeThemes(ctx, userID int64) (light, dark string, err error)`

- [ ] **Step 1: Write the migration**

`internal/db/migrations/101_user_code_themes.sql`:

```sql
-- No CHECK constraint: the theme catalog lives in internal/highlight and
-- unknown values fall back to the defaults when read.
ALTER TABLE users
  ADD COLUMN code_theme_light TEXT NOT NULL DEFAULT 'github',
  ADD COLUMN code_theme_dark  TEXT NOT NULL DEFAULT 'github-dark';
```

Apply it to the test DB: `CZ_DATABASE_DSN="$TEST_DATABASE_DSN" go run ./cmd/cloudzilla migrate`. If the Bash guard refuses `$VAR`, use the literal DSN.

- [ ] **Step 2: Write the failing tests**

Append to `internal/service/user_service_test.go`, adding the `highlight` import (`github.com/mkappworks-dev/cloudzilla-app/internal/highlight`) to the import block:

```go
func TestUserService_CodeThemes_DefaultsThenRoundTrips(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	ctx := context.Background()

	light, dark, err := svc.CodeThemes(ctx, userID)
	if err != nil {
		t.Fatalf("CodeThemes: %v", err)
	}
	if light != highlight.DefaultLight || dark != highlight.DefaultDark {
		t.Errorf("new user themes = %q/%q, want %q/%q", light, dark, highlight.DefaultLight, highlight.DefaultDark)
	}

	if err := svc.UpdateCodeThemes(ctx, userID, "solarized-light", "dracula"); err != nil {
		t.Fatalf("UpdateCodeThemes: %v", err)
	}
	if light, dark, _ = svc.CodeThemes(ctx, userID); light != "solarized-light" || dark != "dracula" {
		t.Errorf("CodeThemes = %q/%q, want solarized-light/dracula", light, dark)
	}
	u, err := svc.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.CodeThemeLight != "solarized-light" || u.CodeThemeDark != "dracula" {
		t.Errorf("GetByID themes = %q/%q, want solarized-light/dracula", u.CodeThemeLight, u.CodeThemeDark)
	}
}

func TestUserService_UpdateCodeThemes_RejectsOtherModesAndUnknownIDs(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	ctx := context.Background()

	if err := svc.UpdateCodeThemes(ctx, userID, "dracula", "no-such-theme"); err != nil {
		t.Fatalf("UpdateCodeThemes: %v", err)
	}
	u, err := svc.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.CodeThemeLight != highlight.DefaultLight || u.CodeThemeDark != highlight.DefaultDark {
		t.Errorf("saved %q/%q, want the defaults", u.CodeThemeLight, u.CodeThemeDark)
	}
}

func TestUserService_CodeThemes_NormalizesStoredIDsOutsideTheCatalog(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	testutil.Exec(t, db, `UPDATE users SET code_theme_light = 'retired', code_theme_dark = 'github' WHERE id = $1`, userID)

	light, dark, err := svc.CodeThemes(context.Background(), userID)
	if err != nil {
		t.Fatalf("CodeThemes: %v", err)
	}
	if light != highlight.DefaultLight || dark != highlight.DefaultDark {
		t.Errorf("CodeThemes = %q/%q, want the defaults", light, dark)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/service/ -run 'CodeThemes' -count=1`
Expected: FAIL to compile, with `svc.CodeThemes undefined`.

- [ ] **Step 4: Add the model fields**

In `internal/model/user.go`, after the `SessionVersion` field of `User`:

```go
	CodeThemeLight     string         `db:"code_theme_light"    json:"-"`
	CodeThemeDark      string         `db:"code_theme_dark"     json:"-"`
```

- [ ] **Step 5: Extend `userColumns` and `scanUser`, and add the store methods**

In `internal/store/user_store.go`, the `userColumns` const ends `… email_verified_at, session_version` and becomes `… email_verified_at, session_version, code_theme_light, code_theme_dark`. The `dest` slice in `scanUser` ends `&u.EmailVerifiedAt, &u.SessionVersion}` and becomes `&u.EmailVerifiedAt, &u.SessionVersion, &u.CodeThemeLight, &u.CodeThemeDark}`.

Add after `UpdateNotificationPrefs`:

```go
func (s *UserStore) UpdateCodeThemes(ctx context.Context, userID int64, light, dark string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET code_theme_light=$1, code_theme_dark=$2, updated_at=NOW() WHERE id=$3`,
		light, dark, userID,
	)
	if err != nil {
		return fmt.Errorf("user update code themes: %w", err)
	}
	return nil
}

func (s *UserStore) GetCodeThemes(ctx context.Context, userID int64) (light, dark string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT code_theme_light, code_theme_dark FROM users WHERE id=$1`, userID,
	).Scan(&light, &dark)
	if err != nil {
		return "", "", fmt.Errorf("user get code themes: %w", err)
	}
	return light, dark, nil
}
```

- [ ] **Step 6: Add the service methods**

In `internal/service/user_service.go`, import `github.com/mkappworks-dev/cloudzilla-app/internal/highlight` and add after `UpdateNotificationPrefs`:

```go
// UpdateCodeThemes saves the user's code theme for each site mode; an ID
// outside that mode's catalog saves the default.
func (s *UserService) UpdateCodeThemes(ctx context.Context, userID int64, light, dark string) error {
	return s.store.UpdateCodeThemes(ctx, userID, highlight.NormalizeLight(light), highlight.NormalizeDark(dark))
}

func (s *UserService) CodeThemes(ctx context.Context, userID int64) (light, dark string, err error) {
	light, dark, err = s.store.GetCodeThemes(ctx, userID)
	if err != nil {
		return "", "", err
	}
	return highlight.NormalizeLight(light), highlight.NormalizeDark(dark), nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/service/ ./internal/store/ -count=1` with `TEST_DATABASE_DSN` set.
Expected: PASS, with the three new tests reported as run, not skipped (check with `-run CodeThemes -v`). The store's existing user tests also pass with the wider `userColumns`.

- [ ] **Step 8: Commit**

```bash
git add internal/db/migrations/101_user_code_themes.sql internal/model/user.go internal/store/user_store.go internal/service/user_service.go internal/service/user_service_test.go
git commit -m "feat(settings): store a code theme per site mode for each user"
```

---

### Task 4: Deliver the themes to every page

**Files:**
- Modify: `internal/view/viewmodels.go`, `internal/handler/page_handler.go`, `internal/view/layout/layout.templ`
- Test: `internal/router/code_theme_test.go` (new)

**Interfaces:**
- Consumes: `UserService.CodeThemes`, `UserService.UpdateCodeThemes` (Task 3); `highlight.DefaultLight/DefaultDark` (Task 2); `/static/code-themes.css` (Task 2).
- Produces:
  - `view.BasePage.CodeLight`, `view.BasePage.CodeDark string`
  - `func (b BasePage) CodeThemeLight() string`, `func (b BasePage) CodeThemeDark() string`
  - `<html … data-code-light="…" data-code-dark="…">`
  - the `hl` container class convention (Tasks 6–8 add it to code containers)

- [ ] **Step 1: Write the failing router test**

Create `internal/router/code_theme_test.go`:

```go
package router_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestLayout_CarriesTheViewersCodeThemes(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	rr := serve(h, browserRequest(http.MethodGet, "/explore", "", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /explore signed out = %d, want 200", rr.Code)
	}
	assertCodeThemes(t, rr.Body.String(), highlight.DefaultLight, highlight.DefaultDark)

	if err := svc.User.UpdateCodeThemes(context.Background(), userID, "solarized-light", "dracula"); err != nil {
		t.Fatalf("UpdateCodeThemes: %v", err)
	}
	rr = serve(h, browserRequest(http.MethodGet, "/explore", makeJWT(t, userID, "testuser_"+suffix), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /explore signed in = %d, want 200", rr.Code)
	}
	assertCodeThemes(t, rr.Body.String(), "solarized-light", "dracula")
	if !strings.Contains(rr.Body.String(), `href="/static/code-themes.css`) {
		t.Error("layout does not link code-themes.css")
	}
}

func assertCodeThemes(t *testing.T, body, light, dark string) {
	t.Helper()
	want := `data-code-light="` + light + `" data-code-dark="` + dark + `"`
	if !strings.Contains(body, want) {
		t.Errorf("<html> lacks %s", want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/router/ -run TestLayout_CarriesTheViewersCodeThemes -count=1` with `TEST_DATABASE_DSN` set.
Expected: FAIL with `<html> lacks data-code-light="github" data-code-dark="github-dark"`.

- [ ] **Step 3: Add the `BasePage` fields and accessors**

In `internal/view/viewmodels.go`, add to `BasePage` after `SetupPending`:

```go
	// CodeLight and CodeDark are the viewer's code themes; read them through CodeThemeLight/CodeThemeDark.
	CodeLight string
	CodeDark  string
```

Add after the struct, importing `github.com/mkappworks-dev/cloudzilla-app/internal/highlight`:

```go
// CodeThemeLight falls back to the default for pages built without basePage(), such as setup and error pages.
func (b BasePage) CodeThemeLight() string {
	if b.CodeLight == "" {
		return highlight.DefaultLight
	}
	return b.CodeLight
}

func (b BasePage) CodeThemeDark() string {
	if b.CodeDark == "" {
		return highlight.DefaultDark
	}
	return b.CodeDark
}
```

- [ ] **Step 4: Fill them in `basePage()`**

In `internal/handler/page_handler.go`, right after `page := BasePage{CurrentUser: &claims, …}`:

```go
	if light, dark, err := services.User.CodeThemes(r.Context(), claims.UserID); err != nil {
		slog.Error("basePage: code theme lookup failed; rendering defaults",
			"error", err, "user_id", claims.UserID, "path", r.URL.Path)
	} else {
		page.CodeLight, page.CodeDark = light, dark
	}
```

- [ ] **Step 5: Update the layout**

In `internal/view/layout/layout.templ`, replace

```templ
	<html lang="en" class="dark">
```

with

```templ
	<html lang="en" class="dark" data-code-light={ base.CodeThemeLight() } data-code-dark={ base.CodeThemeDark() }>
```

Then replace

```templ
			<link rel="stylesheet" href={ assets.URL("/static/main.css") }/>
```

with

```templ
			<link rel="stylesheet" href={ assets.URL("/static/main.css") }/>
			<link rel="stylesheet" href={ assets.URL("/static/code-themes.css") }/>
```

- [ ] **Step 6: Regenerate templ**

Run `make generate-templ` on its own. Then run `grep -c 'code-themes.css' internal/view/layout/layout_templ.go`; expected ≥ 1.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/router/ ./internal/view/... ./internal/handler/ -count=1` with `TEST_DATABASE_DSN` set.
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/view/viewmodels.go internal/handler/page_handler.go internal/view/layout/layout.templ internal/view/layout/layout_templ.go internal/router/code_theme_test.go
git commit -m "feat(ui): put the viewer's code themes on <html> and load their stylesheet"
```

---

### Task 5: Appearance settings

**Files:**
- Modify: `internal/handler/settings_handler.go`, `internal/router/router.go`, `internal/view/pages/settings.templ`, `internal/view/pages/settings_helpers.go`, `internal/router/router_test.go`
- Test: `internal/router/code_theme_test.go` (append)

**Interfaces:**
- Consumes: `UserService.UpdateCodeThemes`/`CodeThemes` (Task 3); `highlight.LightThemes`, `DarkThemes`, `NormalizeLight`, `NormalizeDark`, `Block` (Tasks 1–2); `data.User.CodeThemeLight/Dark` (Task 3 model).
- Produces: `POST /settings/appearance`; `GET /settings/appearance` → 301 `/settings#appearance`; a settings section with `id="appearance"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/router/code_theme_test.go`, adding `"net/url"` to its imports:

```go
func TestAppearanceSettings_SavesAndShowsTheThemes(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	session := makeJWT(t, userID, "testuser_"+suffix)

	req := browserRequest(http.MethodPost, "/settings/appearance", session,
		url.Values{"code_theme_light": {"gruvbox-light"}, "code_theme_dark": {"nord"}})
	req.Header.Set("HX-Request", "true")
	if rr := serve(h, req); rr.Code != http.StatusNoContent {
		t.Fatalf("POST /settings/appearance = %d, want 204", rr.Code)
	}
	light, dark, err := svc.User.CodeThemes(context.Background(), userID)
	if err != nil {
		t.Fatalf("CodeThemes: %v", err)
	}
	if light != "gruvbox-light" || dark != "nord" {
		t.Errorf("saved %q/%q, want gruvbox-light/nord", light, dark)
	}

	rr := serve(h, browserRequest(http.MethodGet, "/settings", session, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /settings = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`id="appearance"`,
		`<option value="gruvbox-light" selected`,
		`<option value="nord" selected`,
		`class="hl-`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
}
```

In `internal/router/router_test.go`:
- add `{"/settings/appearance", "/settings#appearance"},` to the redirect table after the `/settings/notifications` row;
- change `[]string{"/settings/notifications", "/settings/security/setup"}` to `[]string{"/settings/notifications", "/settings/appearance", "/settings/security/setup"}`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/router/ -run 'Appearance|Moved|Redirect' -count=1` with `TEST_DATABASE_DSN` set.
Expected: FAIL. The POST returns 404/405, and the redirect row fails.

- [ ] **Step 3: Add the handler**

Append to `internal/handler/settings_handler.go`:

```go
func (h *Handler) UpdateAppearanceSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := h.Services.User.UpdateCodeThemes(r.Context(), claims.UserID, r.FormValue("code_theme_light"), r.FormValue("code_theme_dark")); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/settings#appearance", http.StatusSeeOther)
}
```

- [ ] **Step 4: Add the routes**

In `internal/router/router.go`, after `r.With(authMW).Post("/settings/notifications", h.UpdateNotificationSettings)`:

```go
	r.With(authMW).Post("/settings/appearance", h.UpdateAppearanceSettings)
```

After `r.With(authMW).Get("/settings/notifications", handler.MovedPermanently("/settings", "notifications"))`:

```go
	r.With(authMW).Get("/settings/appearance", handler.MovedPermanently("/settings", "appearance"))
```

- [ ] **Step 5: Add the preview source**

Append to `internal/view/pages/settings_helpers.go`, adding imports `html/template` and `github.com/mkappworks-dev/cloudzilla-app/internal/highlight`:

```go
const codeThemePreview = `// Greet welcomes name and counts their unread messages.
func Greet(name string, unread int) string {
	if name == "" {
		return "Hello, world!"
	}
	return fmt.Sprintf("Hello, %s! You have %d new messages.", name, unread)
}`

func codeThemePreviewHTML() template.HTML {
	return highlight.Block("go", "", codeThemePreview)
}
```

- [ ] **Step 6: Add the section to `settings.templ`**

Import `github.com/mkappworks-dev/cloudzilla-app/internal/highlight` in `settings.templ`.

In the nav, after `@settingsNavLink("#profile", "Profile", true)`, add:

```templ
					@settingsNavLink("#appearance", "Appearance", false)
```

In the section list, after `@settingsProfileSection(data)`, add:

```templ
				@settingsAppearanceSection(data)
```

Add these components after `settingsProfileSection`. Each select's `onchange` writes the `<html>` attribute, so the preview and every code block on the page restyle before the save round-trip:

```templ
templ settingsAppearanceSection(data view.SettingsData) {
	<section id="appearance" class="border border-border rounded-lg bg-card p-6 scroll-mt-6">
		<h2 class="text-base font-semibold tracking-tight mb-1">Appearance</h2>
		<p class="text-[12.5px] text-muted-foreground mb-6">Colors for code in files, diffs and Markdown. The preview follows the site theme; toggle it to preview the other mode. Changes save automatically.</p>
		<form
			hx-post="/settings/appearance"
			hx-trigger="change"
			hx-swap="none"
			data-toast="Code theme saved"
		>
			@settingsCodeThemeSelect("code_theme_light", "Light mode code theme", "codeLight", highlight.LightThemes(), highlight.NormalizeLight(data.User.CodeThemeLight))
			@settingsCodeThemeSelect("code_theme_dark", "Dark mode code theme", "codeDark", highlight.DarkThemes(), highlight.NormalizeDark(data.User.CodeThemeDark))
		</form>
		<pre class="hl mt-4 rounded-md border border-border px-4 py-3 font-mono text-[12.5px] leading-[1.6] overflow-x-auto"><code>@templ.Raw(string(codeThemePreviewHTML()))</code></pre>
	</section>
}

templ settingsCodeThemeSelect(name, label, dataKey string, themes []highlight.Theme, selected string) {
	<div class="flex items-center justify-between gap-4 py-3 border-b border-border last:border-b-0">
		<label for={ name } class="block text-sm font-medium text-foreground">{ label }</label>
		<div class="w-48 shrink-0">
			@components.Select(templ.Attributes{"id": name, "name": name, "onchange": "document.documentElement.dataset." + dataKey + " = this.value"}) {
				for _, t := range themes {
					<option value={ t.ID } selected?={ t.ID == selected }>{ t.Name }</option>
				}
			}
		</div>
	</div>
}
```

- [ ] **Step 7: Regenerate templ**

Run `make generate-templ` on its own. Then run `grep -c 'settings/appearance' internal/view/pages/settings_templ.go`; expected ≥ 1.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/router/ ./internal/view/... ./internal/handler/ -count=1` with `TEST_DATABASE_DSN` set.
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/handler/settings_handler.go internal/router/router.go internal/router/router_test.go internal/router/code_theme_test.go internal/view/pages/settings.templ internal/view/pages/settings_templ.go internal/view/pages/settings_helpers.go
git commit -m "feat(settings): pick light and dark code themes with a live preview"
```

---

### Task 6: Highlight file views (blob, tree file, blame, gist)

**Files:**
- Create: `internal/view/components/code_text.templ`
- Modify: `internal/service/code_service.go` (`CodeLine`), `internal/service/code_service_tree.go` (`GetBlob`), `internal/service/code_service_blame.go`, `internal/view/components/blame_row.templ`, `internal/view/pages/blob.templ`, `internal/view/pages/tree.templ`, `internal/view/pages/blame.templ`, `internal/view/pages/gist_detail.templ`
- Test: `internal/service/code_service_highlight_test.go` (new)

**Interfaces:**
- Consumes: `highlight.Lines`, `highlight.Block` (Task 1); the `hl` container class (Task 4).
- Produces:
  - `service.CodeLine.HTML template.HTML`; `service.BlameLine.HTML template.HTML`
  - `components.CodeText(text string, html template.HTML)`
  - `components.DiffCode(lineType, content string, html template.HTML)`, used by Task 7
  - the test fixture `cSource` in package `service`, reused by Task 7

- [ ] **Step 1: Write the failing service tests**

Create `internal/service/code_service_highlight_test.go`:

```go
package service

import (
	"strings"
	"testing"
)

// cSource opens a block comment early, so a hunk around line 6 starts inside it.
const cSource = "/* header\n * line 2\n * line 3\n * line 4\n * line 5\n * line 6\n * line 7\n */\nint x = 1;\n"

func TestGetBlob_HighlightsEachLine(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "main.c", cSource)

	res, err := r.svc.GetBlob("alice", "pulls", "main", "main.c")
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if len(res.Lines) != 10 {
		t.Fatalf("lines = %d, want 10", len(res.Lines))
	}
	if !strings.Contains(string(res.Lines[2].HTML), `class="hl-cm"`) {
		t.Errorf("line 3 HTML = %q, want a comment span", res.Lines[2].HTML)
	}
	if !strings.Contains(string(res.Lines[8].HTML), "<span") {
		t.Errorf("line 9 HTML = %q, want token spans", res.Lines[8].HTML)
	}
}

func TestGetBlob_PlainTextFileHasNoHTML(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "notes.txt", "just words\n")

	res, err := r.svc.GetBlob("alice", "pulls", "main", "notes.txt")
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	for _, l := range res.Lines {
		if l.HTML != "" {
			t.Errorf("line %d HTML = %q, want empty", l.Num, l.HTML)
		}
	}
}

func TestGetBlame_HighlightsEachLine(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "main.c", cSource)

	res, err := r.svc.GetBlame("alice", "pulls", "main", "main.c")
	if err != nil {
		t.Fatalf("GetBlame: %v", err)
	}
	if !strings.Contains(string(res.Lines[2].HTML), `class="hl-cm"`) {
		t.Errorf("line 3 HTML = %q, want a comment span", res.Lines[2].HTML)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/service/ -run 'TestGetBlob_|TestGetBlame_' -count=1`
Expected: FAIL to compile, with `res.Lines[2].HTML undefined`.

- [ ] **Step 3: Add `HTML` to `CodeLine` and `BlameLine`, and fill it**

In `internal/service/code_service.go`, import `html/template` and change `CodeLine` to:

```go
type CodeLine struct {
	Num  int
	Text string
	HTML template.HTML `json:"-"`
}
```

In `internal/service/code_service_tree.go` `GetBlob`, import `github.com/mkappworks-dev/cloudzilla-app/internal/highlight` and replace

```go
		rawLines := strings.Split(contents, "\n")
		lines := make([]CodeLine, len(rawLines))
		for i, text := range rawLines {
			lines[i] = CodeLine{Num: i + 1, Text: text}
		}
```

with

```go
		rawLines := strings.Split(contents, "\n")
		html := highlight.Lines(path, contents)
		lines := make([]CodeLine, len(rawLines))
		for i, text := range rawLines {
			lines[i] = CodeLine{Num: i + 1, Text: text}
			if html != nil {
				lines[i].HTML = html[i]
			}
		}
```

In `internal/service/code_service_blame.go`, import `html/template`, `strings` and the `highlight` package. Add `HTML template.HTML \`json:"-"\`` to `BlameLine` after `Text`. In `GetBlame`, after the `for i, l := range blameResult.Lines { … }` loop, add:

```go
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	if html := highlight.Lines(path, strings.Join(texts, "\n")); html != nil {
		for i := range lines {
			lines[i].HTML = html[i]
		}
	}
```

- [ ] **Step 4: Run the service tests to verify they pass**

Run: `go test ./internal/service/ -run 'TestGetBlob_|TestGetBlame_' -count=1`
Expected: PASS.

- [ ] **Step 5: Add the `CodeText` and `DiffCode` components**

Create `internal/view/components/code_text.templ`:

```templ
package components

import "html/template"

// CodeText renders one line of code: highlighted markup when there is some, else the plain text.
templ CodeText(text string, html template.HTML) {
	if html != "" {
		@templ.Raw(string(html))
	} else {
		{ text }
	}
}

// DiffCode renders a diff line's marker and code. Highlighted code keeps the
// marker's add/delete color while its tokens take the code theme's.
templ DiffCode(lineType, content string, html template.HTML) {
	if html != "" {
		<span class={ templ.KV("text-success", lineType == "add"), templ.KV("text-destructive", lineType == "del") }>{ diffMarker(lineType) }</span>@templ.Raw(string(html))
	} else {
		{ diffMarker(lineType) }{ content }
	}
}

func diffMarker(lineType string) string {
	switch lineType {
	case "add":
		return "+"
	case "del":
		return "-"
	}
	return " "
}
```

- [ ] **Step 6: Use them in the file views**

`internal/view/pages/blob.templ`:
- `<div class="overflow-x-auto bg-card" x-ref="code">` → `<div class="hl overflow-x-auto bg-card" x-ref="code">`
- `<td class="pl-4 pr-4 py-0 whitespace-pre">{ line.Text }</td>` → `<td class="pl-4 pr-4 py-0 whitespace-pre">@components.CodeText(line.Text, line.HTML)</td>`

`internal/view/pages/tree.templ` (the inline file view, around lines 131–140) gets the same two edits.

`internal/view/pages/blame.templ`:
- line ~59, the `<div class="overflow-x-auto">` wrapping the blame table → `<div class="hl overflow-x-auto">`
- in the `components.BlameLine{…}` literal, after `Code:        line.Text,` add `CodeHTML:    line.HTML,`

`internal/view/components/blame_row.templ`:
- add `"html/template"` to its imports
- add `CodeHTML    template.HTML` to `BlameLine` after `Code`
- `<td class="pl-4 pr-4 py-1.5 whitespace-pre">{ line.Code }</td>` → `<td class="pl-4 pr-4 py-1.5 whitespace-pre">@CodeText(line.Code, line.CodeHTML)</td>`

`internal/view/pages/gist_detail.templ`:
- add `"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"` to its imports
- `<pre class="px-4 py-3 font-mono text-[12.5px] leading-[1.6] overflow-x-auto"><code>{ f.Content }</code></pre>` → `<pre class="hl px-4 py-3 font-mono text-[12.5px] leading-[1.6] overflow-x-auto"><code>@components.CodeText(f.Content, highlight.Block("", f.Filename, f.Content))</code></pre>`

- [ ] **Step 7: Regenerate templ and build**

Run `make generate-templ` on its own. Then run `grep -c 'CodeText' internal/view/pages/blob_templ.go internal/view/pages/tree_templ.go internal/view/pages/gist_detail_templ.go internal/view/components/blame_row_templ.go`; each should be ≥ 1. Then run `go build ./... && go test ./internal/view/... ./internal/service/ ./internal/handler/ -count=1`.
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/service/code_service.go internal/service/code_service_tree.go internal/service/code_service_blame.go internal/service/code_service_highlight_test.go internal/view/components/code_text.templ internal/view/components/code_text_templ.go internal/view/components/blame_row.templ internal/view/components/blame_row_templ.go internal/view/pages/blob.templ internal/view/pages/blob_templ.go internal/view/pages/tree.templ internal/view/pages/tree_templ.go internal/view/pages/blame.templ internal/view/pages/blame_templ.go internal/view/pages/gist_detail.templ internal/view/pages/gist_detail_templ.go
git commit -m "feat(code): syntax-highlight file, blame and gist views"
```

---

### Task 7: Highlight diffs with whole-file context

**Files:**
- Create: `internal/service/code_service_highlight.go`
- Modify: `internal/service/code_service_commit.go` (`DiffLine`, `FileDiff`, `GetCommit`), `internal/service/code_service_merge.go` (`GetPullDiff`), `internal/handler/page_repo_handler.go` (commit page), `internal/handler/page_pull_handler.go` (PR files page), `internal/view/components/diff_table.templ`, `internal/view/pages/pr_files.templ`
- Test: `internal/service/code_service_highlight_test.go` (append)

**Interfaces:**
- Consumes: `highlight.NewBudget`, `highlight.LinesWithin`, `highlight.MaxBytes` (Task 1); `components.CodeText`, `components.DiffCode`, `cSource`, `newPullRepo` (Task 6 and existing tests).
- Produces:
  - `service.DiffLine.HTML template.HTML`
  - unexported `FileDiff.oldBlob`, `FileDiff.newBlob plumbing.Hash`
  - `func (s *CodeService) HighlightDiffs(owner, repoName string, files []FileDiff)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/service/code_service_highlight_test.go`, adding imports `github.com/go-git/go-git/v5/plumbing`:

```go
func headOf(t *testing.T, r *pullRepo, branch string) string {
	t.Helper()
	ref, err := r.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatalf("resolve %s: %v", branch, err)
	}
	return ref.Hash().String()
}

// A hunk around line 6 starts at " * line 3", inside a comment opened on line 1.
func commentEditCommit(t *testing.T) (*pullRepo, *CommitDetail) {
	t.Helper()
	r := newPullRepo(t)
	r.commit(t, "main", "main.c", cSource)
	r.commit(t, "main", "main.c", strings.Replace(cSource, " * line 6", " * line six", 1))
	detail, err := r.svc.GetCommit("alice", "pulls", headOf(t, r, "main"))
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	return r, detail
}

func TestHighlightDiffs_ColorsHunksWithWholeFileContext(t *testing.T) {
	t.Parallel()
	r, detail := commentEditCommit(t)
	r.svc.HighlightDiffs("alice", "pulls", detail.Files)

	hunk := detail.Files[0].Hunks[0]
	if hunk.Lines[0].Content != " * line 3" {
		t.Fatalf("hunk starts at %q, want \" * line 3\"", hunk.Lines[0].Content)
	}
	for _, l := range hunk.Lines {
		if strings.HasPrefix(l.Content, " *") && !strings.Contains(string(l.HTML), `class="hl-cm"`) {
			t.Errorf("%s line %q HTML = %q, want a comment span", l.Type, l.Content, l.HTML)
		}
	}
}

func TestHighlightDiffs_LeavesLinesThatDisagreeWithTheBlobPlain(t *testing.T) {
	t.Parallel()
	r, detail := commentEditCommit(t)
	lines := detail.Files[0].Hunks[0].Lines
	lines[0].Content = "not what the blob says"
	r.svc.HighlightDiffs("alice", "pulls", detail.Files)

	if lines[0].HTML != "" {
		t.Errorf("mismatched line HTML = %q, want empty", lines[0].HTML)
	}
	if lines[1].HTML == "" {
		t.Error("matching line HTML is empty, want highlighted")
	}
}

func TestHighlightDiffs_PullDiffWithNewFile(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "README.md", "# x\n")
	if err := r.svc.CreateBranch("alice", "pulls", "feature", "main"); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	r.commit(t, "feature", "main.go", "package main\n\nfunc main() {}\n")

	d, err := r.svc.GetPullDiff("alice", "pulls", "main", "feature")
	if err != nil {
		t.Fatalf("GetPullDiff: %v", err)
	}
	r.svc.HighlightDiffs("alice", "pulls", d.Files)
	for _, l := range d.Files[0].Hunks[0].Lines {
		if l.Content != "" && l.HTML == "" {
			t.Errorf("added line %q has no HTML", l.Content)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/service/ -run TestHighlightDiffs -count=1`
Expected: FAIL to compile, with `r.svc.HighlightDiffs undefined`.

- [ ] **Step 3: Record blob hashes and add `DiffLine.HTML`**

In `internal/service/code_service_commit.go`, import `html/template`. Then:

```go
type DiffLine struct {
	Type    string // "add", "del", "ctx"
	Content string
	OldNum  int
	NewNum  int
	HTML    template.HTML `json:"-"`
}
```

Add the two fields as the last fields of `FileDiff`:

```go
	oldBlob  plumbing.Hash
	newBlob  plumbing.Hash
```

In `GetCommit`, right after the `fd := FileDiff{…}` literal:

```go
		if from != nil {
			fd.oldBlob = from.Hash()
		}
		if to != nil {
			fd.newBlob = to.Hash()
		}
```

In `internal/service/code_service_merge.go` `GetPullDiff`, add the identical block right after its `fd := FileDiff{…}` literal.

The `buildHunks` call `DiffLine{opMap[l.op], l.content, l.oldNum, l.newNum}` is positional and won't compile with the new field. Change it to keyed fields: `DiffLine{Type: opMap[l.op], Content: l.content, OldNum: l.oldNum, NewNum: l.newNum}`.

- [ ] **Step 4: Implement `internal/service/code_service_highlight.go`**

```go
package service

import (
	"html/template"
	"io"
	"log/slog"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
)

const (
	diffHighlightBytes = 4 << 20
	diffHighlightTime  = 2 * time.Second
)

// HighlightDiffs fills DiffLine.HTML for files. It highlights each side's
// whole blob, since a hunk can begin inside a block comment or string. It is
// opt-in because GetCommit and GetPullDiff also serve post-receive stats and
// CODEOWNERS lookups, which must not pay for it.
func (s *CodeService) HighlightDiffs(owner, repoName string, files []FileDiff) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		slog.Warn("highlight diffs: open repo failed", "owner", owner, "repo", repoName, "error", err)
		return
	}
	budget := highlight.NewBudget(diffHighlightBytes, diffHighlightTime)
	for i := range files {
		f := &files[i]
		if f.IsBinary || len(f.Hunks) == 0 {
			continue
		}
		oldSide := highlightBlob(repo, budget, f.OldPath, f.oldBlob)
		newSide := highlightBlob(repo, budget, f.NewPath, f.newBlob)
		for h := range f.Hunks {
			for l := range f.Hunks[h].Lines {
				line := &f.Hunks[h].Lines[l]
				if line.Type == "del" {
					line.HTML = oldSide.at(line.OldNum, line.Content)
				} else {
					line.HTML = newSide.at(line.NewNum, line.Content)
				}
			}
		}
	}
}

type highlightedBlob struct {
	src  []string
	html []template.HTML
}

// at returns line num's HTML only when the blob's line is text, so a diff
// that disagrees with the blob renders plain rather than showing other code.
func (b highlightedBlob) at(num int, text string) template.HTML {
	if num < 1 || num > len(b.html) || b.src[num-1] != text {
		return ""
	}
	return b.html[num-1]
}

func highlightBlob(repo *gogit.Repository, budget *highlight.Budget, path string, hash plumbing.Hash) highlightedBlob {
	if hash.IsZero() {
		return highlightedBlob{}
	}
	blob, err := repo.BlobObject(hash)
	if err != nil {
		slog.Warn("highlight diffs: read blob failed", "path", path, "blob", hash, "error", err)
		return highlightedBlob{}
	}
	if blob.Size > highlight.MaxBytes {
		return highlightedBlob{}
	}
	r, err := blob.Reader()
	if err != nil {
		slog.Warn("highlight diffs: open blob failed", "path", path, "blob", hash, "error", err)
		return highlightedBlob{}
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		slog.Warn("highlight diffs: read blob failed", "path", path, "blob", hash, "error", err)
		return highlightedBlob{}
	}
	src := string(data)
	html := highlight.LinesWithin(budget, path, src)
	if html == nil {
		return highlightedBlob{}
	}
	return highlightedBlob{src: strings.Split(src, "\n"), html: html}
}
```

- [ ] **Step 5: Run the service tests to verify they pass**

Run: `go test ./internal/service/ -count=1`
Expected: PASS, including the new `TestHighlightDiffs_*` tests and all existing diff tests.

- [ ] **Step 6: Call it from the two diff pages**

In `internal/handler/page_repo_handler.go` (the commit page), right after the `commit, err := h.Services.Code.GetCommit(owner, repoName, sha)` error check:

```go
	h.Services.Code.HighlightDiffs(owner, repoName, commit.Files)
```

In `internal/handler/page_pull_handler.go` (the PR files page), change the `GetPullDiff` block to:

```go
	diff, err := h.Services.Code.GetPullDiff(owner, repoName, pull.BaseBranch, pull.HeadBranch)
	if err != nil {
		slog.Warn("pull files: get pull diff failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
		diff = &service.PRDiffResult{}
		loadErrFiles = true
	} else {
		h.Services.Code.HighlightDiffs(owner, repoName, diff.Files)
	}
```

- [ ] **Step 7: Render highlighted diff lines**

`internal/view/components/diff_table.templ` (`DiffHunkTable`):
- the outer `<div class="overflow-x-auto">` → `<div class="hl overflow-x-auto">`
- `<td class="pl-2 pr-4 py-0.5 whitespace-pre align-top">{ line.Content }</td>` → `<td class="pl-2 pr-4 py-0.5 whitespace-pre align-top">@CodeText(line.Content, line.HTML)</td>`

`internal/view/pages/pr_files.templ`:

1. Each of the three table wrappers `<div class="overflow-x-auto">` (lines ~295, ~357, ~383: in `prFileDiff`, `prFileSplitDiff`, `prFileWholeDiff`) → `<div class="hl overflow-x-auto">`.

2. The unified code cell in `prFileDiff` (~line 311) and the one in `prFileWholeDiff` (~line 391) each read:

```templ
							<td class={ "pl-3 pr-4 py-0.5 whitespace-pre", templ.KV("text-success", line.Type == "add"), templ.KV("text-destructive", line.Type == "del"), templ.KV("text-foreground/85", line.Type != "add" && line.Type != "del") }>
								if line.Type == "add" {
									+{ line.Content }
								} else if line.Type == "del" {
									-{ line.Content }
								} else {
									{ " " }{ line.Content }
								}
							</td>
```

Replace each with:

```templ
							<td class={ "pl-3 pr-4 py-0.5 whitespace-pre", templ.KV("text-success", line.Type == "add" && line.HTML == ""), templ.KV("text-destructive", line.Type == "del" && line.HTML == ""), templ.KV("text-foreground/85", line.Type != "add" && line.Type != "del" && line.HTML == "") }>@components.DiffCode(line.Type, line.Content, line.HTML)</td>
```

3. In `prSplitCell`, replace the code `<td class={ … }> if c.Type == "add" { +{ c.Content } } … </td>` with:

```templ
		<td class={ "pl-3 pr-4 py-0.5 whitespace-pre overflow-hidden", templ.KV("border-l border-border", right), templ.KV("bg-success/10", c.Type == "add"), templ.KV("bg-destructive/10", c.Type == "del"), templ.KV("text-success", c.Type == "add" && c.HTML == ""), templ.KV("text-destructive", c.Type == "del" && c.HTML == ""), templ.KV("text-foreground/85", c.Type == "ctx" && c.HTML == "") }>@components.DiffCode(c.Type, c.Content, c.HTML)</td>
```

4. Add `HTML template.HTML` to `prSplitCellData` after `Content` (import `"html/template"` in `pr_files.templ` if it isn't imported). Then, in each `prSplitCellData{…}` literal in the split-row builder (four literals, ~lines 450–475), add the matching `HTML:` value next to `Content:`: `HTML: l.HTML` for both ctx cells, `HTML: dels[j].HTML` and `HTML: adds[j].HTML`. Search for any other `prSplitCellData{` literal in the file and do the same.

- [ ] **Step 8: Regenerate templ and run the full suite**

Run `make generate-templ` on its own. Then run `grep -c 'DiffCode' internal/view/pages/pr_files_templ.go` (expected ≥ 1). Then run `go build ./... && go test ./... -count=1` with `TEST_DATABASE_DSN` set.
Expected: PASS. Existing PR-files tests that assert `text-success`/`text-destructive` on code cells may need updating only where the line is now highlighted. Check that each such failure is that case and not a regression before changing it, and report every assertion changed.

- [ ] **Step 9: Commit**

```bash
git add internal/service/code_service_commit.go internal/service/code_service_merge.go internal/service/code_service_highlight.go internal/service/code_service_highlight_test.go internal/handler/page_repo_handler.go internal/handler/page_pull_handler.go internal/view/components/diff_table.templ internal/view/components/diff_table_templ.go internal/view/pages/pr_files.templ internal/view/pages/pr_files_templ.go
git commit -m "feat(diff): syntax-highlight commit and pull request diffs"
```

Also `git add` any test file changed in Step 8.

---

### Task 8: Highlight fenced Markdown code, and document highlighting

**Files:**
- Modify: `internal/markdown/markdown.go`, `internal/markdown/markdown_test.go`, `docs/code-browser.md`

**Interfaces:**
- Consumes: `highlight.Block` (Task 1).
- Produces: fenced blocks in a known language render as `<pre class="hl"><code class="language-<lang>">…spans…</code></pre>`.

- [ ] **Step 1: Update and add the failing tests**

In `internal/markdown/markdown_test.go`, in the table that holds the code block cases (~lines 450–590):

1. Change the `"fenced code with language"` case's `want` to check for highlighting. The table compares exact strings, so give this case its own test instead: delete it from the table and add the test below. Do the same for `"tilde fence with extra info"` (python), `"info string with entity"` (c++), `"unclosed fence"` (js) and `"fenced code in list"` (sh). Keep every other case, including `"empty fenced block"`, the mermaid cases, `"info string with html"`, `"mermaid is case sensitive"` and `"indented code block"`, byte-for-byte unchanged. They pin the plain fallback.

2. Add:

```go
func TestRender_HighlightsFencedCodeInAKnownLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, src, lang string
	}{
		{"go", "```go\nfmt.Println(\"<hi>\", 'x', a&b)\n```", "go"},
		{"tilde with extra info", "~~~python linenos=1\nprint('hi')\n~~~", "python"},
		{"entity in info string", "```c&#43;&#43;\nint x;\n```", "c++"},
		{"unclosed fence", "```js\nlet x = 1;", "js"},
		{"in a list", "- item\n\n  ```sh\n  echo \"hi\"\n  ```", "sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Render(tc.src)
			if !strings.Contains(got, `<pre class="hl"><code class="language-`+tc.lang+`">`) {
				t.Errorf("Render = %q, want a highlighted <pre class=\"hl\"> for %s", got, tc.lang)
			}
			if !strings.Contains(got, `<span class="hl-`) {
				t.Errorf("Render = %q, want token spans", got)
			}
		})
	}
}

func TestRender_HighlightedCodeIsEscaped(t *testing.T) {
	got := Render("```go\nvar s = \"</code></pre><script>alert(1)</script>\"\n```")
	if strings.Contains(got, "<script>") {
		t.Errorf("Render = %q, want the script tag escaped", got)
	}
}
```

Check whether `strings` is already imported in `markdown_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/markdown/ -count=1`
Expected: FAIL. The new tests find no `<pre class="hl">`; the remaining table cases still pass.

- [ ] **Step 3: Implement the decorator**

In `internal/markdown/markdown.go`, import `github.com/mkappworks-dev/cloudzilla-app/internal/highlight` and add:

```go
// highlightCode renders fenced blocks in a known language as token spans. It
// decides on entering and remembers the choice, because a highlight can fall
// back to plain at its deadline and the exit call must close what was opened.
func highlightCode() ghtml.NodeRendererDecorator {
	highlighted := map[ast.Node]bool{}
	return func(next ghtml.NodeRenderer) ghtml.NodeRenderer {
		return ghtml.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
			if !entering {
				if highlighted[node] {
					return ast.WalkContinue, nil
				}
				return next.Render(w, source, node, entering, rc)
			}
			n := node.(*ast.CodeBlock)
			lang, ok := n.Language(source)
			if !ok || n.CodeBlockKind != ast.CodeBlockKindFenced {
				return next.Render(w, source, node, entering, rc)
			}
			code := highlight.Block(lang, "", n.Value.Str(source))
			if code == "" {
				return next.Render(w, source, node, entering, rc)
			}
			highlighted[node] = true
			bw := w.(util.BufWriter)
			_, _ = bw.WriteString(`<pre class="hl"><code class="language-`)
			_, _ = ghtml.ContextTextWriter(rc).WriteString(lang)
			_, _ = bw.WriteString(`">`)
			_, _ = bw.WriteString(string(code))
			_, _ = bw.WriteString("</code></pre>\n")
			return ast.WalkSkipChildren, nil
		})
	}
}
```

In `Render`, register it **before** the mermaid decorator. goldmark applies a later decorator on top of an earlier one, so mermaid stays outermost and keeps claiming `mermaid` fences:

```go
	r := ghtml.New(
		ghtml.WithHardWraps(),
		ghtml.WithExtensions(extension.GFMHTMLRenderer),
		ghtml.WithNodeRendererDecorator(ast.KindCodeBlock, highlightCode()),
		ghtml.WithNodeRendererDecorator(ast.KindCodeBlock, renderMermaid),
	)
```

If `ghtml.NodeRendererDecorator` is not the exported name of the decorator func type in goldmark v2, use the type that `WithNodeRendererDecorator`'s second parameter declares (check `go doc github.com/yuin/goldmark/v2/renderer/html WithNodeRendererDecorator`). Delete the duplicated first doc-comment line above `Render` ("Render converts markdown src to safe HTML. Mermaid fenced blocks are…"). Merge it into a single comment: `// Render converts Markdown source to safe HTML, sanitizing links and disabling raw HTML. Mermaid fences become <pre class="mermaid"> for mermaid.js; fences in a known language are syntax-highlighted.`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/markdown/ -count=1 -v`
Expected: PASS. All mermaid cases are unchanged. `"mermaid is case sensitive"` (`Mermaid`) stays plain only if chroma has no `mermaid` lexer. If it now highlights, report it to the controller instead of editing the expectation.

- [ ] **Step 5: Document highlighting**

Append to `docs/code-browser.md`:

```markdown
## Syntax highlighting

`internal/highlight` runs Chroma on the server and emits `<span class="hl-<token>">` with no inline colors. `cmd/server/frontend/static/code-themes.css` holds every catalog theme, scoped by `html.dark[data-code-dark="…"]` or `html:not(.dark)[data-code-light="…"]`. The layout writes the viewer's saved pair (Settings → Appearance, `users.code_theme_light/dark`) onto `<html>`, so the site's light/dark toggle switches code themes with no script. Code containers carry the `hl` class for the theme's background.

- **Where:** `GetBlob` and `GetBlame` fill `CodeLine.HTML`/`BlameLine.HTML`; the gist page and `internal/markdown` (fenced blocks in a known language) call `highlight.Block`. Diffs are highlighted only when a page calls `CodeService.HighlightDiffs`: `GetCommit` and `GetPullDiff` also serve post-receive stats and CODEOWNERS, which must not pay for it.
- **Diffs** highlight both sides' whole blobs and map lines by number, because a hunk can start inside a block comment. A line gets HTML only when the blob's line equals `DiffLine.Content`; anything else renders plain.
- **Caps:** sources over 512 KiB, calls over 500 ms, and diff pages past 4 MiB / 2 s render plain. Lexers are picked by filename, plus shebang sniffing for extensionless scripts.
- **Themes:** the catalog is `highlight.Themes`. After changing it, run `make generate-code-themes`; a test fails while the committed CSS is stale. Stored IDs outside the catalog read back as the defaults (`github` / `github-dark`).
```

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1` with `TEST_DATABASE_DSN` set.
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/markdown/markdown.go internal/markdown/markdown_test.go docs/code-browser.md
git commit -m "feat(markdown): syntax-highlight fenced code blocks"
```
