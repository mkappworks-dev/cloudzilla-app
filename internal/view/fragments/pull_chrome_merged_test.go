package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func renderChrome(t *testing.T, d PullChromeData) string {
	t.Helper()
	d.OwnerName = "o"
	d.Repo = &model.Repository{Name: "r"}
	var buf bytes.Buffer
	if err := PullChrome(d, templ.NopComponent).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPullChrome_MergedHeaderNamesTheMergerAndTheCommitsThatMerged(t *testing.T) {
	out := renderChrome(t, PullChromeData{
		Pull:           &model.PullRequest{Number: 4, Title: "T", State: model.PRStateMerged, BaseBranch: "main", HeadBranch: "feat", AuthorName: "niko"},
		AuthorUsername: "niko", MergedBy: "ada", MergedCommits: 3, CommitsCount: 0,
	})
	for _, want := range []string{">@ada</a>", " merged ", "3 commits"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, ">@niko</a>") || strings.Contains(out, "0 commits") {
		t.Errorf("header still names the author or 0 commits: %s", out)
	}
}

func TestPullChrome_OpenAndOlderMergedPRsKeepTheirAuthorAndLiveCount(t *testing.T) {
	open := renderChrome(t, PullChromeData{
		Pull:           &model.PullRequest{Number: 4, State: model.PRStateOpen, BaseBranch: "main", HeadBranch: "feat"},
		AuthorUsername: "niko", MergedBy: "ada", MergedCommits: 3, CommitsCount: 2,
	})
	if !strings.Contains(open, ">@niko</a>") || !strings.Contains(open, "2 commits") {
		t.Errorf("an open PR is about its author and live commits: %s", open)
	}
	older := renderChrome(t, PullChromeData{
		Pull:           &model.PullRequest{Number: 4, State: model.PRStateMerged, BaseBranch: "main", HeadBranch: "feat"},
		AuthorUsername: "niko", CommitsCount: 1,
	})
	if !strings.Contains(older, ">@niko</a>") || !strings.Contains(older, "1 commit") {
		t.Errorf("a merge with no recorded merger or count falls back to author and live count: %s", older)
	}
}
