package pages_test

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// The issues handler matches q against text only, so the page must not
// advertise qualifiers until it parses them.
func TestIssues_SearchPlaceholderMatchesTabWithoutQualifiers(t *testing.T) {
	for _, state := range []string{"open", "closed"} {
		out := render(t, pages.Issues(view.IssuesData{Owner: "alice", RepoName: "r", StateFilter: state}))
		if want := `placeholder="Search ` + state + ` issues"`; !strings.Contains(out, want) {
			t.Errorf("%s tab: missing %s", state, want)
		}
		for _, claim := range []string{"is:open", "label:bug", "milestone:v", "author:", "Filter syntax"} {
			if strings.Contains(out, claim) {
				t.Errorf("%s tab advertises unsupported qualifier %q", state, claim)
			}
		}
	}
}

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
