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
