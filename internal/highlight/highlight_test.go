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
