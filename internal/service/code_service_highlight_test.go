package service

import (
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"
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

func TestHighlightDiffs_DeletedFileTakesTheOldBlob(t *testing.T) {
	t.Parallel()
	svc, work, workDir, _ := mergeabilityTestRepo(t, "alice", "pulls")
	renameDefaultToMain(t, work)
	commitFile(t, work, workDir, "main.go", "package main\n\nfunc main() {}\n", "add")
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Remove("main.go"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: time.Now().UTC()}
	head, err := wt.Commit("delete", &gogit.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	pushBranch(t, work, "main")

	detail, err := svc.GetCommit("alice", "pulls", head.String())
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	if len(detail.Files) != 1 || !detail.Files[0].IsDelete {
		t.Fatalf("files = %+v, want one deleted file", detail.Files)
	}
	svc.HighlightDiffs("alice", "pulls", detail.Files)
	for _, l := range detail.Files[0].Hunks[0].Lines {
		if l.Type != "del" {
			t.Errorf("line %q type = %s, want del", l.Content, l.Type)
		}
		if l.Content != "" && !strings.Contains(string(l.HTML), "<span") {
			t.Errorf("deleted line %q HTML = %q, want token spans from the old blob", l.Content, l.HTML)
		}
	}
}

func TestHighlightDiffs_BinaryFileGetsNoHTML(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "blob.c", "int x;\x00\x01\n")
	r.commit(t, "main", "blob.c", "int y;\x00\x02\n")
	detail, err := r.svc.GetCommit("alice", "pulls", headOf(t, r, "main"))
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	if len(detail.Files) != 1 || !detail.Files[0].IsBinary {
		t.Fatalf("files = %+v, want one binary file", detail.Files)
	}
	r.svc.HighlightDiffs("alice", "pulls", detail.Files)
	for _, h := range detail.Files[0].Hunks {
		for _, l := range h.Lines {
			if l.HTML != "" {
				t.Errorf("binary line %q HTML = %q, want empty", l.Content, l.HTML)
			}
		}
	}
}

// Wrapping the file in a comment shifts every context line down one, and only
// the new side colors them as comment.
func TestHighlightDiffs_ContextLinesUseTheNewSide(t *testing.T) {
	t.Parallel()
	const body = "int a;\nint b;\nint c;\n"
	r := newPullRepo(t)
	r.commit(t, "main", "main.c", body)
	r.commit(t, "main", "main.c", "/*\n"+body+"*/\n")
	detail, err := r.svc.GetCommit("alice", "pulls", headOf(t, r, "main"))
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	r.svc.HighlightDiffs("alice", "pulls", detail.Files)
	ctx := 0
	for _, l := range detail.Files[0].Hunks[0].Lines {
		if l.Type != "ctx" {
			continue
		}
		ctx++
		if l.OldNum == l.NewNum {
			t.Errorf("ctx line %q has OldNum == NewNum == %d, want them shifted", l.Content, l.OldNum)
		}
		if !strings.Contains(string(l.HTML), `class="hl-cm"`) {
			t.Errorf("ctx line %q HTML = %q, want the new side's comment span", l.Content, l.HTML)
		}
	}
	if ctx != 3 {
		t.Errorf("ctx lines = %d, want 3", ctx)
	}
}
