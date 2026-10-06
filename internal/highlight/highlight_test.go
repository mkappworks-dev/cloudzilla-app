package highlight

import (
	"context"
	"html"
	"html/template"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
	if got := Lines("deploy", "#!/bin/sh\necho hi\n"); got == nil {
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

func TestBlockWithin_StopsAtTheByteBudget(t *testing.T) {
	src := "package main\n\nvar x = 1\n" // 24 bytes
	b := NewBudget(40, time.Minute)
	if got := BlockWithin(b, "go", "", src); got == "" {
		t.Fatal("first block = \"\", want highlighted")
	}
	if got := BlockWithin(b, "go", "", src); got != "" {
		t.Error("second block highlighted past the budget, want plain")
	}
}

func TestBlockWithin_DoesNotChargePlainSources(t *testing.T) {
	b := NewBudget(30, time.Minute)
	for _, lang := range []string{"text", "nosuchlang"} {
		if got := BlockWithin(b, lang, "", strings.Repeat("x", 20)); got != "" {
			t.Fatalf("BlockWithin(%q) = %q, want plain", lang, got)
		}
	}
	if got := BlockWithin(b, "", "notes.txt", strings.Repeat("x", 20)); got != "" {
		t.Fatalf("BlockWithin(notes.txt) = %q, want plain", got)
	}
	if got := BlockWithin(b, "go", "", "package main\n\nvar x = 1\n"); got == "" {
		t.Error("Go block = \"\", want highlighted: plain sources should not use the budget")
	}
}

func TestBlockWithin_PicksTheLexerLikeBlock(t *testing.T) {
	b := NewBudget(1<<20, time.Minute)
	if got := BlockWithin(b, "", "main.py", "print(1)\n"); got == "" {
		t.Error(`BlockWithin("", "main.py") = "", want highlighted by filename`)
	}
	if got, want := BlockWithin(b, "go", "", "x := 1\n"), Block("go", "", "x := 1\n"); got != want {
		t.Errorf("BlockWithin = %q, want Block's %q", got, want)
	}
}

func TestBlockWithin_GivesUpPastTheBudgetDeadline(t *testing.T) {
	b := NewBudget(1<<20, -time.Second)
	if got := BlockWithin(b, "go", "", "package main\n"); got != "" {
		t.Errorf("BlockWithin = %q, want plain once the budget's deadline has passed", got)
	}
}

func TestBlockWithin_ExpiredBudgetIsNotCharged(t *testing.T) {
	b := NewBudget(100, -time.Second)
	if got := BlockWithin(b, "go", "", "package main\n"); got != "" {
		t.Errorf("BlockWithin = %q, want plain once the budget's deadline has passed", got)
	}
	if b.bytes != 100 {
		t.Errorf("budget bytes = %d, want 100: an expired budget must refuse before lexing", b.bytes)
	}
}

func TestSub_ChargesTheParentToo(t *testing.T) {
	src := "package main\n\nvar x = 1\n" // 24 bytes
	parent := NewBudget(40, time.Minute)
	if got := BlockWithin(parent.Sub(30, time.Minute), "go", "", src); got == "" {
		t.Fatal("first block = \"\", want highlighted")
	}
	if got := BlockWithin(parent.Sub(30, time.Minute), "go", "", src); got != "" {
		t.Error("second block highlighted past the parent's budget, want plain")
	}
	if parent.bytes != 16 {
		t.Errorf("parent bytes = %d, want 16", parent.bytes)
	}
}

func TestSub_RefusalLeavesEveryBudgetAlone(t *testing.T) {
	parent := NewBudget(100, time.Minute)
	child := parent.Sub(10, time.Minute)
	if got := BlockWithin(child, "go", "", "package main\n\nvar x = 1\n"); got != "" {
		t.Fatalf("BlockWithin = %q, want plain past the child's budget", got)
	}
	if parent.bytes != 100 || child.bytes != 10 {
		t.Errorf("bytes = %d/%d, want 100/10 after a refusal", parent.bytes, child.bytes)
	}
}

func TestSub_GivesUpPastTheParentDeadline(t *testing.T) {
	child := NewBudget(1<<20, -time.Second).Sub(1<<20, time.Minute)
	if got := BlockWithin(child, "go", "", "package main\n"); got != "" {
		t.Errorf("BlockWithin = %q, want plain once the parent's deadline has passed", got)
	}
}

func TestSub_OfNilIsAPlainBudget(t *testing.T) {
	var b *Budget
	if got := BlockWithin(b.Sub(1<<20, time.Minute), "go", "", "package main\n"); got == "" {
		t.Error("BlockWithin = \"\", want highlighted")
	}
}

func TestBudgetFrom(t *testing.T) {
	if b := BudgetFrom(context.Background()); b != nil {
		t.Errorf("BudgetFrom(empty ctx) = %p, want nil", b)
	}
	b := NewBudget(1, time.Minute)
	if got := BudgetFrom(WithBudget(context.Background(), b)); got != b {
		t.Errorf("BudgetFrom = %p, want %p", got, b)
	}
}

func TestBudget_ConcurrentChargesNeverOverspend(t *testing.T) {
	src := "package main\n\nvar x = 1\n" // 24 bytes
	parent := NewBudget(24*10, time.Minute)
	var wg sync.WaitGroup
	var highlighted atomic.Int32
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if BlockWithin(parent.Sub(1<<20, time.Minute), "go", "", src) != "" {
				highlighted.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := highlighted.Load(); n != 10 {
		t.Errorf("%d blocks highlighted, want 10: the budget fits exactly 10", n)
	}
}

func TestBlock_ReturnsWhenTheLexerEmitsOnlyEmptyTokens(t *testing.T) {
	for _, src := range []string{"{\n", `"`, "<"} {
		if !returnsWithin(2*time.Second, func() { Block("jungle", "", src) }) {
			t.Fatalf(`Block("jungle", %q) did not return`, src)
		}
	}
}

func TestLines_MergesAdjacentRunsOfTheSameClass(t *testing.T) {
	// The Go lexer emits a raw string's backtick and body as separate tokens.
	got := Lines("main.go", "x := `raw\nstr`\n")
	if n := strings.Count(string(got[0]), `<span class="hl-s">`); n != 1 {
		t.Errorf("line 1 = %q, want one string span", got[0])
	}
	if want := template.HTML(`<span class="hl-s">str` + "`" + `</span>`); got[1] != want {
		t.Errorf("line 2 = %q, want %q", got[1], want)
	}
}

func TestLinesWithin_DoesNotChargeSourcesOverMaxBytes(t *testing.T) {
	big := "package main\n" + strings.Repeat("// x\n", MaxBytes/5)
	b := NewBudget(len(big), time.Minute)
	if got := LinesWithin(b, "big.go", big); got != nil {
		t.Fatal("source over MaxBytes highlighted, want nil")
	}
	if got := LinesWithin(b, "a.go", "package main\n\nvar x = 1\n"); got == nil {
		t.Error("Go file = nil, want highlighted: an unhighlighted source should not use the budget")
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
