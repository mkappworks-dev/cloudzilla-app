package service

import (
	"errors"
	"testing"
)

func TestApplySuggestions_OneCommitForEveryEditBottomUp(t *testing.T) {
	r := newTipTestRepo(t)
	before := branchTip(t, r.repo, "feature")

	// The first edit grows f.txt by a line; applying top-down would leave line 2 pointing at the wrong text.
	upd, err := r.svc.ApplySuggestions(r.owner, r.name, "feature", []SuggestionEdit{
		{Path: "f.txt", Line: 1, Replacement: "uno\nuno-bis"},
		{Path: "a.txt", Line: 1, Replacement: "A"},
		{Path: "f.txt", Line: 2, Replacement: "dos"},
	}, tipTestAuthor)
	if err != nil {
		t.Fatalf("ApplySuggestions: %v", err)
	}
	for path, want := range map[string]string{"f.txt": "uno\nuno-bis\ndos\n", "a.txt": "A\n"} {
		got, err := r.svc.GetRawBlob(r.owner, r.name, "feature", path)
		if err != nil || string(got) != want {
			t.Errorf("%s = %q (%v), want %q", path, got, err, want)
		}
	}
	if upd.Old != before || branchTip(t, r.repo, "feature") != upd.New {
		t.Errorf("want one commit on top of %s, got %s -> %s", before, upd.Old, upd.New)
	}
	commit, err := r.repo.CommitObject(upd.New)
	if err != nil || len(commit.ParentHashes) != 1 || commit.ParentHashes[0] != before {
		t.Errorf("batch must be a single commit on the old tip: %v %v", commit, err)
	}
}

func TestApplySuggestions_TwoEditsOnOneLineChangeNothing(t *testing.T) {
	r := newTipTestRepo(t)
	before := branchTip(t, r.repo, "feature")

	_, err := r.svc.ApplySuggestions(r.owner, r.name, "feature", []SuggestionEdit{
		{Path: "f.txt", Line: 1, Replacement: "uno"},
		{Path: "f.txt", Line: 1, Replacement: "eins"},
	}, tipTestAuthor)
	if !errors.Is(err, ErrSuggestionConflict) {
		t.Fatalf("want ErrSuggestionConflict, got %v", err)
	}
	if got := branchTip(t, r.repo, "feature"); got != before {
		t.Errorf("feature moved to %s, want it left at %s", got, before)
	}
}
