package pages

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

type CommitDayGroup struct {
	Date    time.Time
	Commits []service.CommitSummary
}

func commitDays(commits []service.CommitSummary) []CommitDayGroup {
	var groups []CommitDayGroup
	for _, c := range commits {
		day := c.AuthorTime.UTC().Truncate(24 * time.Hour)
		if len(groups) > 0 && groups[len(groups)-1].Date.Equal(day) {
			groups[len(groups)-1].Commits = append(groups[len(groups)-1].Commits, c)
		} else {
			groups = append(groups, CommitDayGroup{Date: day, Commits: []service.CommitSummary{c}})
		}
	}
	return groups
}

// Non-nil so JSLiteral renders [] rather than null, which Alpine's .includes would throw on.
func selectedReviewerList(opts []components.ReviewerOption) []string {
	sel := []string{}
	for _, o := range opts {
		if o.Selected {
			sel = append(sel, o.Username)
		}
	}
	return sel
}
