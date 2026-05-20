package pages

import "github.com/mkappworks-dev/cloudzilla-app/internal/view"

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

func milestoneItemsLen(data view.MilestoneDetailData) int {
	if data.Tab == "pulls" {
		return len(data.Pulls)
	}
	return len(data.Issues)
}

func milestoneAuthor(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}
