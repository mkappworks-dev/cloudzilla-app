package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var (
	asWriter = accessFor(Writer)
	asReader = accessFor(Reader)
)

func issuesData(b view.BasePage, state, raw string, issues []model.Issue, open, closed int) view.IssuesData {
	d := view.IssuesData{
		BasePage: onTrackerRepo(b, "issues", asWriter), Repo: trackerRepo(), Issues: issues, Owner: fixtureUser,
		RepoName: trackerRepoName, IssueLabels: trackerIssueLabels(), CommentCounts: trackerCommentCounts(), StateFilter: state,
		OpenTabURL: trackerBasePath + "/issues", ClosedTabURL: trackerBasePath + "/issues?q=is%3Aclosed",
		Filter: issueFilter(raw, len(issues), b.CurrentUser != nil), ClearURL: trackerBasePath + "/issues",
		Page: 1, TotalPages: 1, OpenCount: open, ClosedCount: closed,
	}
	return d
}

func issueDetailData(b view.BasePage, a access, iss model.Issue) view.IssueDetailData {
	ms := trackerMilestones()
	pulls := trackerPulls()
	var current *model.Milestone
	if iss.MilestoneID != nil {
		current = &ms[0]
	}
	return view.IssueDetailData{
		BasePage: onTrackerRepo(b, "issues", a), Repo: trackerRepo(), Issue: iss, Owner: fixtureUser, RepoName: trackerRepoName,
		Comments:  trackerComments(),
		BodyHTML:  "<p>After a week of uptime the tile index keeps every entry it has ever seen. Memory climbs until the agent is restarted.</p><ul><li>Fleet of 3 nodes</li><li>Retention window left at the default</li></ul>",
		Labels:    []model.Label{labelBug},
		Assignees: trackerAssignees(), AllLabels: trackerLabel, Milestone: current, AllMilestones: ms[:2],
		LinkedPRs: pulls[:1], RepoPulls: pulls, Collaborators: trackerCollaborators(),
		CanWrite: a.canWrite, CanManage: a.canManage,
		ThreadSubscription: view.ThreadSubscriptionData{Owner: fixtureUser, RepoName: trackerRepoName, Segment: "issues", Number: iss.Number,
			State: subscribedIf(a.canWrite), Reason: model.ThreadReasonAuthor},
	}
}

func trackerPages() []page {
	return []page{
		{
			Name: "issues", Route: "/{owner}/{repo}/issues", Templ: "issues.templ", Title: "Issues",
			Variants: []variant{
				{Note: "open issues", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := issuesData(b, "open", "", trackerIssues(), 4, 3)
					d.PinnedIssues = trackerIssues()[:1]
					return pages.Issues(d)
				}},
				{Name: "closed", Note: "the Closed tab", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := issuesData(b, "closed", "is:closed", trackerClosedIssues(), 4, 3)
					d.PinnedIssues = trackerIssues()[:1]
					return pages.Issues(d)
				}},
				{Name: "filtered", Note: "filter chips, with the Label menu open", Viewer: Writer,
					Transform: openFilterMenu("Label"), Render: func(b view.BasePage) templ.Component {
						is := trackerIssues()
						return pages.Issues(issuesData(b, "open", "label:bug sort:updated", []model.Issue{is[0], is[3]}, 4, 3))
					}},
				{Name: "no-match", Note: "filters that match nothing", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Issues(issuesData(b, "open", "label:bug author:ada-silva label:documentation", nil, 4, 3))
				}},
				{Name: "notice", Note: "an unrecognized qualifier, searched as text, with no match", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Issues(issuesData(b, "open", "sort:comments-desc tile", nil, 4, 3))
				}},
				{Name: "empty", Note: "no issues in any state", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := issuesData(b, "open", "", nil, 0, 0)
					d.RepoEmpty = true
					return pages.Issues(d)
				}},
			},
		},
		{
			Name: "issue_detail", Route: "/{owner}/{repo}/issues/{number}", Templ: "issue_detail.templ", Title: "Issue",
			Variants: []variant{
				{Note: "open, for a maintainer", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					iss := trackerIssues()[0]
					iss.MilestoneID = int64Ptr(1)
					return pages.IssueDetail(issueDetailData(b, asWriter, iss))
				}},
				{Name: "closed", Note: "closed by a pull request", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					iss := trackerIssues()[0]
					iss.State = model.IssueStateClosed
					d := issueDetailData(b, asWriter, iss)
					d.Events = []model.IssueEvent{{ID: 1, IssueID: iss.ID, ActorName: "ada-silva", Type: model.IssueEventClosed,
						CreatedAt: ago(days(8)), SourceOwner: fixtureUser, SourceRepo: trackerRepoName, PullNumber: 58}}
					return pages.IssueDetail(d)
				}},
				{Name: "locked", Note: "locked, viewed by someone who cannot comment", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					iss := trackerIssues()[0]
					iss.IsLocked = true
					return pages.IssueDetail(issueDetailData(b, asReader, iss))
				}},
				{Name: "locked-maintainer", Note: "locked, viewed by a maintainer, who can still comment", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					iss := trackerIssues()[0]
					iss.IsLocked = true
					return pages.IssueDetail(issueDetailData(b, asWriter, iss))
				}},
				{Name: "viewer", Note: "signed in without write access: no edit controls", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.IssueDetail(issueDetailData(b, asReader, trackerIssues()[0]))
				}},
				{Name: "anon", Note: "signed out: no comment box", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.IssueDetail(issueDetailData(b, access{}, trackerIssues()[0]))
				}},
			},
		},
		{
			Name: "issue_new", Route: "/{owner}/{repo}/issues/new", Templ: "issue_new.templ", Title: "New issue",
			Variants: []variant{
				{Note: "the form, with the metadata pickers", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.IssueNew(issueNewData(b, asWriter, true))
				}},
				{Name: "chooser", Note: "the repository has issue templates", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.IssueNew(issueNewData(b, asWriter, false))
				}},
				{Name: "error", Note: "the title was left blank", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := issueNewData(b, asWriter, true)
					d.Error = "Title is required."
					return pages.IssueNew(d)
				}},
				{Name: "viewer", Note: "without write access: placeholder metadata", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.IssueNew(issueNewData(b, asReader, true))
				}},
			},
		},
	}
}

func issueNewData(b view.BasePage, a access, form bool) view.IssueNewData {
	d := view.IssueNewData{
		BasePage: onTrackerRepo(b, "issues", a), Repo: trackerRepo(), Owner: fixtureUser, RepoName: trackerRepoName,
		Templates: []service.IssueTemplate{{Slug: "bug_report", Name: "bug report", Body: "**Steps to reproduce**\n\n1. "},
			{Slug: "feature_request", Name: "feature request", Body: "**What problem does this solve?**\n\n"}},
		ShowForm: form, CanWrite: a.canWrite,
	}
	if a.canWrite {
		d.Collaborators = trackerCollaborators()
		d.Labels = trackerLabel
		d.Milestones = trackerMilestones()[:2]
		d.RepoPulls = []view.LinkedPull{{Number: 58, Title: "Evict stale tile index entries", State: "open"}}
	}
	return d
}
