package main

import (
	"database/sql"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func milestonesData(b view.BasePage, a access, all []model.Milestone) view.MilestonesData {
	d := view.MilestonesData{BasePage: onTrackerRepo(b, "issues", a), Repo: trackerRepo(), Owner: fixtureUser,
		RepoName: trackerRepoName, CanWrite: a.canWrite}
	for _, m := range all {
		if m.State == "closed" {
			d.ClosedMilestones = append(d.ClosedMilestones, m)
		} else {
			d.OpenMilestones = append(d.OpenMilestones, m)
		}
	}
	return d
}

func milestoneDetailData(b view.BasePage, a access, m model.Milestone, tab, state string) view.MilestoneDetailData {
	d := view.MilestoneDetailData{
		BasePage: onTrackerRepo(b, "issues", a), Repo: trackerRepo(), Owner: fixtureUser, RepoName: trackerRepoName,
		Milestone: m, CanWrite: a.canWrite, Tab: tab, State: state, Page: 1, TotalPages: 1, PerPage: 20,
		IssueOpenCount: m.OpenCount, IssueClosedCount: m.ClosedCount, PullOpenCount: 1, PullClosedCount: 1,
		DescriptionHTML: "<p>" + m.Description + "</p>",
	}
	switch {
	case tab == "issues" && state == "open":
		d.Issues = trackerIssues()[:3]
	case tab == "issues":
		d.Issues = trackerClosedIssues()
	case state == "open":
		d.Pulls = trackerPulls()[:1]
	default:
		d.Pulls = trackerPulls()[1:]
	}
	d.TotalCount = len(d.Issues) + len(d.Pulls)
	return d
}

func discussionDetailData(b view.BasePage, a access, disc model.Discussion, replies []view.RenderedDiscussionReply) view.DiscussionDetailData {
	return view.DiscussionDetailData{
		BasePage: onTrackerRepo(b, "discussions", a), Repo: trackerRepo(), Owner: fixtureUser, RepoName: trackerRepoName,
		Discussion: disc, Category: catQA, AllCategories: trackerCategories(), Labels: []model.Label{labelDocs},
		AllLabels: trackerLabel, Replies: replies, Participants: []string{"tomas-lindqvist", fixtureUser, "ada-silva"},
		OPReactions: []model.ReactionSummary{{Emoji: "👍", Count: 2}},
		BodyHTML:    "<p>The agent has to go through a corporate proxy. Which environment variable does the sync client read?</p>",
		CanWrite:    a.canWrite,
		ThreadSubscription: view.ThreadSubscriptionData{Owner: fixtureUser, RepoName: trackerRepoName, Segment: "discussions", Number: disc.Number,
			State: subscribedIf(a.canWrite), Reason: model.ThreadReasonAuthor},
	}
}

func discussionReplies(answer bool) []view.RenderedDiscussionReply {
	mk := func(id int64, author, body string, isAnswer bool, age int) view.RenderedDiscussionReply {
		return view.RenderedDiscussionReply{
			DiscussionReply: model.DiscussionReply{ID: id, DiscussionID: 401, AuthorName: author, Body: body, IsAnswer: isAnswer,
				ParentID: sql.NullInt64{}, CreatedAt: ago(days(age)), UpdatedAt: ago(days(age))},
			BodyHTML:  "<p>" + body + "</p>",
			Reactions: []model.ReactionSummary{{Emoji: "👍", Count: 1}},
		}
	}
	return []view.RenderedDiscussionReply{
		mk(501, "ada-silva", "Have you tried setting HTTPS_PROXY before starting the agent?", false, 11),
		mk(502, fixtureUser, "The sync client reads HTTPS_PROXY and NO_PROXY, as documented in the operator guide.", answer, 10),
	}
}

func discussionPages() []page {
	disc := func() model.Discussion { return trackerDiscussions()[0] }
	return []page{
		{
			Name: "milestones", Route: "/{owner}/{repo}/milestones", Templ: "milestones.templ", Title: "Milestones",
			Variants: []variant{
				{Note: "open and closed milestones; the Closed tab switches in the browser", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Milestones(milestonesData(b, asWriter, trackerMilestones()))
				}},
				{Name: "viewer", Note: "without write access: no New milestone button", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.Milestones(milestonesData(b, asReader, trackerMilestones()))
				}},
				{Name: "empty", Note: "no milestones yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Milestones(milestonesData(b, asWriter, nil))
				}},
			},
		},
		{
			Name: "milestone_detail", Route: "/{owner}/{repo}/milestones/{number}", Templ: "milestone_detail.templ", Title: "Milestone",
			Variants: []variant{
				{Note: "open milestone, open issues", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneDetail(milestoneDetailData(b, asWriter, trackerMilestones()[0], "issues", "open"))
				}},
				{Name: "closed-issues", Note: "the Closed filter on the issues tab", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneDetail(milestoneDetailData(b, asWriter, trackerMilestones()[0], "issues", "closed"))
				}},
				{Name: "pulls", Note: "the pull requests tab", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneDetail(milestoneDetailData(b, asWriter, trackerMilestones()[0], "pulls", "open"))
				}},
				{Name: "closed", Note: "a closed milestone", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneDetail(milestoneDetailData(b, asWriter, trackerMilestones()[2], "issues", "closed"))
				}},
				{Name: "viewer", Note: "without write access: no edit or delete", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneDetail(milestoneDetailData(b, asReader, trackerMilestones()[0], "issues", "open"))
				}},
			},
		},
		{
			Name: "milestone_new", Route: "/{owner}/{repo}/milestones/new", Templ: "milestone_new.templ", Title: "New milestone",
			Variants: []variant{
				{Note: "empty form", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneNew(view.MilestoneNewData{BasePage: onTrackerRepo(b, "issues", asWriter), Repo: trackerRepo(),
						Owner: fixtureUser, RepoName: trackerRepoName, CanWrite: true})
				}},
				{Name: "error", Note: "the title was left blank", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.MilestoneNew(view.MilestoneNewData{BasePage: onTrackerRepo(b, "issues", asWriter), Repo: trackerRepo(),
						Owner: fixtureUser, RepoName: trackerRepoName, CanWrite: true, Error: "Title is required.",
						Description: "Fill the gaps in the operator guide.", DueDate: "2025-07-15"})
				}},
			},
		},
		{
			Name: "discussions", Route: "/{owner}/{repo}/discussions", Templ: "discussions.templ", Title: "Discussions",
			Variants: []variant{
				{Note: "open discussions, all categories", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Discussions(trackerDiscussionsData(b, asWriter, "open", 0))
				}},
				{Name: "answered", Note: "the Answered tab", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Discussions(trackerDiscussionsData(b, asWriter, "answered", 0))
				}},
				{Name: "closed", Note: "the Closed tab: locked, unanswered", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Discussions(trackerDiscussionsData(b, asWriter, "closed", 0))
				}},
				{Name: "category", Note: "?category=, one category selected", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Discussions(trackerDiscussionsData(b, asWriter, "open", catIdeas.ID))
				}},
				{Name: "viewer", Note: "without write access: no New discussion button", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.Discussions(trackerDiscussionsData(b, asReader, "open", 0))
				}},
				{Name: "empty", Note: "a category with no open discussions", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Discussions(trackerDiscussionsData(b, asWriter, "open", catShow.ID))
				}},
			},
		},
		{
			Name: "discussion_detail", Route: "/{owner}/{repo}/discussions/{number}", Templ: "discussion_detail.templ", Title: "Discussion",
			Variants: []variant{
				{Note: "open, for a maintainer", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.DiscussionDetail(discussionDetailData(b, asWriter, disc(), discussionReplies(false)))
				}},
				{Name: "answered", Note: "a reply is marked as the answer", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := disc()
					d.IsAnswered = true
					return pages.DiscussionDetail(discussionDetailData(b, asWriter, d, discussionReplies(true)))
				}},
				{Name: "locked", Note: "locked: no reply box for a reader", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					d := disc()
					d.IsLocked = true
					return pages.DiscussionDetail(discussionDetailData(b, asReader, d, discussionReplies(false)))
				}},
				{Name: "viewer", Note: "signed in without write access", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.DiscussionDetail(discussionDetailData(b, asReader, disc(), discussionReplies(false)))
				}},
				{Name: "anon", Note: "signed out: sign in to reply", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.DiscussionDetail(discussionDetailData(b, access{}, disc(), discussionReplies(false)))
				}},
			},
		},
		{
			Name: "discussion_new", Route: "/{owner}/{repo}/discussions/new", Templ: "discussion_new.templ", Title: "New discussion",
			Variants: []variant{
				{Note: "category picked, empty form", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.DiscussionNew(discussionNewData(b, catQA.ID))
				}},
				{Name: "error", Note: "the title was left blank", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := discussionNewData(b, catQA.ID)
					d.Error = "Title is required."
					d.Body = "Which environment variable does the sync client read?"
					return pages.DiscussionNew(d)
				}},
				{Name: "no-categories", Note: "the repository has no categories", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := discussionNewData(b, 0)
					d.Categories = nil
					return pages.DiscussionNew(d)
				}},
			},
		},
	}
}

func discussionNewData(b view.BasePage, active int64) view.DiscussionNewData {
	return view.DiscussionNewData{BasePage: onTrackerRepo(b, "discussions", asWriter), Repo: trackerRepo(), Owner: fixtureUser,
		RepoName: trackerRepoName, Categories: trackerCategories(), ActiveCategoryID: active}
}

// subscribedIf draws the maintainer, who started the thread, as subscribed and every other viewer as not.
func subscribedIf(b bool) string {
	if b {
		return model.ThreadStateSubscribed
	}
	return ""
}
