package pages_test

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestCodeSearch_CopyMatchesScopeAndHidesRank(t *testing.T) {
	empty := render(t, pages.CodeSearch(view.CodeSearchData{}))
	if !strings.Contains(empty, "repositories you can read") || strings.Contains(empty, "public repositories") {
		t.Errorf("empty state should say it searches repositories you can read")
	}

	out := render(t, pages.CodeSearch(view.CodeSearchData{
		Query: "main", Total: 1, Page: 1,
		Results: []model.CodeSearchResult{{OwnerName: "a", RepoName: "r", FilePath: "main.go", Snippet: "func main", Rank: 0.0607927}},
	}))
	if strings.Contains(out, "0.061") {
		t.Errorf("result row shows the raw search rank")
	}
	if !strings.Contains(out, "1 result matching") {
		t.Errorf("summary should read \"1 result matching\"")
	}
}

func TestIssueDetail_StateBadgeIsCapitalised(t *testing.T) {
	out := render(t, pages.IssueDetail(view.IssueDetailData{Owner: "a", RepoName: "r", Issue: model.Issue{State: "open"}}))
	if !strings.Contains(out, ">Open</span>") {
		t.Errorf("issue state badge should read \"Open\" like the lists")
	}
}
