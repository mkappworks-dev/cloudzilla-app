package pages

import (
	"strconv"
	"strings"
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

// pct returns the integer percentage of part out of total as a string.
// Returns "0" when total is zero to avoid division by zero.
func pct(part, total int) string {
	if total == 0 {
		return "0"
	}
	return strconv.Itoa(part * 100 / total)
}

// jsStringList renders a string slice as a single-quoted JS array literal, for
// seeding Alpine x-data state from server values (e.g. pre-selected reviewers).
func jsStringList(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// selectedReviewerList returns the usernames of reviewer options marked selected,
// used to preserve the picker's state when the new-PR form re-renders on error.
func selectedReviewerList(opts []components.ReviewerOption) []string {
	var sel []string
	for _, o := range opts {
		if o.Selected {
			sel = append(sel, o.Username)
		}
	}
	return sel
}
