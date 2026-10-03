package service

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/filemode"
)

// commitDocs gives branch a docs/ directory beside a docs.md file, and returns
// the entries it added. Git orders docs.md first: it compares a directory's
// name as if it ended in "/".
func (r *tipTestRepo) commitDocs(t *testing.T, branch string) map[string]mergeFile {
	t.Helper()
	guide := mergeFile{hash: r.blob(t, "guide\n"), mode: filemode.Regular}
	docs := mergeFile{hash: r.blob(t, "docs\n"), mode: filemode.Regular}
	r.commitEntry(t, branch, "docs/guide.md", guide.mode, guide.hash)
	r.commitEntry(t, branch, "docs.md", docs.mode, docs.hash)
	return map[string]mergeFile{"docs/guide.md": guide, "docs.md": docs}
}

func TestCommitFile_SortsEntriesLikeGit(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
	}{
		{"file beside its directory", []string{"docs/guide.md", "docs.md"}},
		{"directory beside its file", []string{"docs.md", "docs/guide.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			want := map[string]mergeFile{}
			for _, p := range tt.paths {
				r.commit(t, "main", p, p+"\n")
				want[p] = mergeFile{hash: r.blob(t, p+"\n"), mode: filemode.Regular}
			}
			r.expectEntries(t, "main", want)
		})
	}
}

func TestApplySuggestion_SortsEntriesLikeGit(t *testing.T) {
	r := newTipTestRepo(t)
	want := r.commitDocs(t, "feature")
	if err := r.svc.ApplySuggestion(r.owner, r.name, "feature", "f.txt", 1, "uno", tipTestAuthor); err != nil {
		t.Fatalf("ApplySuggestion: %v", err)
	}
	want["f.txt"] = mergeFile{hash: r.blob(t, "uno\ntwo\n"), mode: filemode.Regular}
	r.expectEntries(t, "feature", want)
}

func TestSaveProfileReadme_SortsEntriesLikeGit(t *testing.T) {
	r := newTipTestRepo(t)
	want := r.commitDocs(t, "main")
	if err := r.svc.SaveProfileReadme(r.owner, r.name, "main", "# Alice\n", tipTestAuthor, ""); err != nil {
		t.Fatalf("SaveProfileReadme: %v", err)
	}
	want["README.md"] = mergeFile{hash: r.blob(t, "# Alice\n"), mode: filemode.Regular}
	r.expectEntries(t, "main", want)
}

func TestPullMerges_SortEntriesLikeGit(t *testing.T) {
	for _, tt := range pullMerges {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			want := r.commitDocs(t, "main")
			if err := tt.merge(r); err != nil {
				t.Fatalf("merge: %v", err)
			}
			want["f.txt"] = mergeFile{hash: r.blob(t, "one\ntwo\n"), mode: filemode.Regular}
			r.expectEntries(t, "main", want)
		})
	}
}
