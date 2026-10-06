package pages

import (
	"sort"
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func linkedPullsView(owner, repoName string, pulls []model.PullRequest) []view.LinkedPull {
	out := make([]view.LinkedPull, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, view.LinkedPull{Number: p.Number, Title: p.Title, State: string(p.State),
			OtherRepo: view.OtherRepo(owner, repoName, p.RepoOwner, p.RepoName)})
	}
	return out
}

type issueTimelineEntry struct {
	When    time.Time
	Comment *view.RenderedComment
	Event   *model.IssueEvent
}

// issueTimeline interleaves comments and events by time; a comment and an event
// from the same instant keep that order.
func issueTimeline(comments []view.RenderedComment, events []model.IssueEvent) []issueTimelineEntry {
	out := make([]issueTimelineEntry, 0, len(comments)+len(events))
	for i := range comments {
		out = append(out, issueTimelineEntry{When: comments[i].CreatedAt, Comment: &comments[i]})
	}
	for i := range events {
		out = append(out, issueTimelineEntry{When: events[i].CreatedAt, Event: &events[i]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].When.Before(out[j].When) })
	return out
}

// issueEventSource is what closed the issue, as link text and URL: the PR,
// named "#N" in the issue's own repo, or the commit. Both are empty for a
// close or reopen by hand.
func issueEventSource(owner, repoName string, ev model.IssueEvent) (text, href string) {
	if ev.Type != model.IssueEventClosed || ev.SourceOwner == "" {
		return "", ""
	}
	repoPath := ev.SourceOwner + "/" + ev.SourceRepo
	switch {
	case ev.PullNumber > 0:
		num := "#" + strconv.Itoa(ev.PullNumber)
		if other := view.OtherRepo(owner, repoName, ev.SourceOwner, ev.SourceRepo); other != "" {
			return other + num, "/" + repoPath + "/pulls/" + strconv.Itoa(ev.PullNumber)
		}
		return num, "/" + repoPath + "/pulls/" + strconv.Itoa(ev.PullNumber)
	case len(ev.CommitSHA) >= 7:
		return ev.CommitSHA[:7], "/" + repoPath + "/commit/" + ev.CommitSHA
	}
	return "", ""
}

func issueEventVerb(t string) string {
	if t == model.IssueEventReopened {
		return "reopened this"
	}
	return "closed this"
}

func issueEventVariant(t string) components.TimelineVariant {
	if t == model.IssueEventReopened {
		return components.TimelineSuccess
	}
	return components.TimelineDestructive
}
