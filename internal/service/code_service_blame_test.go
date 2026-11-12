package service

import "testing"

func TestBlobAndBlameCountTheSameLines(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "a.txt", "one\n\nthree\n")

	blob, err := r.svc.GetBlob("alice", "pulls", "main", "a.txt")
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	blame, err := r.svc.GetBlame("alice", "pulls", "main", "a.txt")
	if err != nil {
		t.Fatalf("GetBlame: %v", err)
	}
	if len(blob.Lines) != 3 || len(blame.Lines) != 3 {
		t.Errorf("blob has %d lines, blame has %d, want 3 each", len(blob.Lines), len(blame.Lines))
	}
	if got := blame.Lines[0].AuthorName; got != tipTestAuthor.Name {
		t.Errorf("blame AuthorName = %q, want %q", got, tipTestAuthor.Name)
	}
}
