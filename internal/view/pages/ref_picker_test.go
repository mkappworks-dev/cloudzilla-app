package pages

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

var (
	pickerBranches = []service.BranchInfo{{Name: "main", IsDefault: true}, {Name: "feature-x"}}
	pickerTags     = []service.TagInfo{{Name: "v1.0"}}
)

// Switching refs from a code-browser page keeps the viewer on the same path.
func TestCodeBrowserPages_RefPickerKeepsPath(t *testing.T) {
	tests := []struct {
		name string
		page templ.Component
		want []string
	}{
		{
			name: "blob",
			page: Blob(view.BlobData{Owner: "o", RepoName: "r", Ref: "main", Path: "lib/config.js", Branches: pickerBranches, Tags: pickerTags}),
			want: []string{`href="/o/r/blob/main/lib/config.js"`, `href="/o/r/blob/feature-x/lib/config.js"`, `href="/o/r/blob/v1.0/lib/config.js"`},
		},
		{
			name: "blame",
			page: Blame(view.BlameData{Owner: "o", RepoName: "r", Ref: "main", Path: "lib/config.js", Branches: pickerBranches, Tags: pickerTags}),
			want: []string{`href="/o/r/blame/feature-x/lib/config.js"`, `href="/o/r/blame/v1.0/lib/config.js"`},
		},
		{
			name: "tree root",
			page: Tree(view.TreeData{Owner: "o", RepoName: "r", Ref: "main", RefsURL: "/o/r/refs", Branches: pickerBranches, Tags: pickerTags}),
			want: []string{`href="/o/r/tree/feature-x"`, `href="/o/r/tree/v1.0"`},
		},
		{
			name: "tree subdirectory",
			page: Tree(view.TreeData{Owner: "o", RepoName: "r", Ref: "main", Path: "lib", RefsURL: "/o/r/refs", Branches: pickerBranches, Tags: pickerTags}),
			want: []string{`href="/o/r/tree/feature-x/lib"`},
		},
		{
			name: "tree file view",
			page: Tree(view.TreeData{Owner: "o", RepoName: "r", Ref: "main", Path: "lib/config.js", RefsURL: "/o/r/refs", Branches: pickerBranches, Tags: pickerTags, FileView: &view.TreeFileView{FileName: "config.js"}}),
			want: []string{`href="/o/r/tree/feature-x/lib/config.js"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderHTML(t, tt.page)
			if !strings.Contains(out, `aria-label="Switch branch or tag, current: main"`) {
				t.Fatalf("no ref picker on the page")
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %s", w)
				}
			}
			if !strings.Contains(out, `href="/o/r/refs"`) {
				t.Errorf("missing View all branches link")
			}
		})
	}
}

func TestRepoHome_RefPickerSendsDefaultBranchHome(t *testing.T) {
	out := renderHTML(t, Repo(view.RepoData{
		Repo:     model.Repository{DefaultBranch: "main"},
		Owner:    "o",
		RepoName: "r",
		Branches: pickerBranches,
		Tags:     pickerTags,
	}))
	for _, w := range []string{`href="/o/r" role="menuitem"`, `href="/o/r/tree/feature-x"`, `href="/o/r/tree/v1.0"`} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %s", w)
		}
	}
}
