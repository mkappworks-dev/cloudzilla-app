package service

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
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
