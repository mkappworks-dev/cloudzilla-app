package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// A private issue is visible to its author and writers only, but its card sits
// on a board that anyone who can read the repository opens.
func TestProjectService_Board_HidesPrivateIssueFromViewersWhoCannotSeeIt(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Board")
	col := e.column(t, p.ID, "Todo")

	issue := func(author int64, title, visibility string) int64 {
		i := &model.Issue{RepoID: e.repoID, AuthorID: author, Title: title, State: model.IssueStateOpen, Visibility: visibility}
		if err := e.issues.Create(ctx, i); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, &i.ID, nil, ""); err != nil {
			t.Fatal(err)
		}
		return i.ID
	}
	issue(e.ownerID, "public roadmap", "public")
	issue(e.otherID, "reporter's private bug", "private")
	issue(e.ownerID, "embargoed fix", "private")

	ptr := func(id int64) *int64 { return &id }
	titles := func(viewer *int64) []string {
		views, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID, viewer)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range views[0].Cards {
			if c.Kind == "hidden" {
				if c.Title != "" || c.Number != 0 || c.State != "" {
					t.Errorf("hidden card leaks %+v", c)
				}
				out = append(out, "<hidden>")
				continue
			}
			out = append(out, c.Title)
		}
		return out
	}
	eq := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	for name, tc := range map[string]struct {
		viewer *int64
		want   []string
	}{
		"anonymous":          {nil, []string{"public roadmap", "<hidden>", "<hidden>"}},
		"reader":             {ptr(e.otherID + 1<<30), []string{"public roadmap", "<hidden>", "<hidden>"}},
		"author of one":      {ptr(e.otherID), []string{"public roadmap", "reporter's private bug", "<hidden>"}},
		"writer":             {ptr(e.writerID), []string{"public roadmap", "reporter's private bug", "embargoed fix"}},
		"repository owner":   {ptr(e.ownerID), []string{"public roadmap", "reporter's private bug", "embargoed fix"}},
		"admin collaborator": {ptr(e.adminID), []string{"public roadmap", "reporter's private bug", "embargoed fix"}},
	} {
		if got := titles(tc.viewer); !eq(got, tc.want...) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
