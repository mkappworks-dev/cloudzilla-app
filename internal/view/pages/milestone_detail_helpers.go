package pages

import "github.com/mkappworks-dev/cloudzilla-app/internal/view"

// milestoneStateCount returns the count badge value for an Open/Closed filter
// button, scoped to whichever tab is active.
func milestoneStateCount(data view.MilestoneDetailData, state string) int {
	if data.Tab == "pulls" {
		if state == "closed" {
			return data.PullClosedCount
		}
		return data.PullOpenCount
	}
	if state == "closed" {
		return data.IssueClosedCount
	}
	return data.IssueOpenCount
}

// milestoneItemsLen returns the number of rows rendered for the active tab.
func milestoneItemsLen(data view.MilestoneDetailData) int {
	if data.Tab == "pulls" {
		return len(data.Pulls)
	}
	return len(data.Issues)
}

// milestoneAuthor falls back to a placeholder when an author name is missing.
func milestoneAuthor(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}
