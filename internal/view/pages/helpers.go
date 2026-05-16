package pages

import (
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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
