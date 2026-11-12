package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func prVariant(name, note string, o pullOpts) variant {
	return variant{Name: name, Note: note, Viewer: o.Who.viewer(), Render: func(b view.BasePage) templ.Component {
		return pages.PullDetail(pullDetailData(b, o))
	}}
}

// prVariants crosses the merge-box states with the three signed-in viewers an open pull request distinguishes: the
// footer, the review prompt and the sidebar controls branch on write access and authorship.
func prVariants() []variant {
	states := []struct {
		merge pullMerge
		note  string
	}{
		{mergeReadyState, "ready to merge"},
		{mergeNeedsState, "review required"},
		{mergePendingState, "waiting on required checks"},
		{mergeFailingState, "a required check failed"},
		{mergeChangesState, "a reviewer requested changes"},
		{mergeConflictState, "merge conflicts"},
		{mergeAutoState, "auto-merge armed"},
	}
	who := []struct {
		who    pullWho
		suffix string
		note   string
	}{
		{whoReviewer, "", "writer who did not open it"},
		{whoAuthor, "author", "its author"},
		{whoReadonly, "readonly", "signed in without write access"},
	}
	var out []variant
	for _, s := range states {
		for _, w := range who {
			name := string(s.merge)
			if s.merge == mergeReadyState {
				name = ""
			}
			if w.suffix != "" {
				if name != "" {
					name += "-"
				}
				name += w.suffix
			}
			out = append(out, prVariant(name, "open, "+s.note+"; "+w.note, pullOpts{State: model.PRStateOpen, Merge: s.merge, Who: w.who}))
		}
	}
	out = append(out,
		prVariant("anon", "open, ready to merge; signed out", pullOpts{State: model.PRStateOpen, Merge: mergeReadyState, Who: whoAnon}),
		prVariant("draft", "draft; writer who did not open it", pullOpts{State: model.PRStateOpen, Draft: true, Merge: mergeDraftState, Who: whoReviewer}),
		prVariant("draft-readonly", "draft; signed in without write access", pullOpts{State: model.PRStateOpen, Draft: true, Merge: mergeDraftState, Who: whoReadonly}),
		prVariant("merged", "merged; writer", pullOpts{State: model.PRStateMerged, Who: whoReviewer}),
		prVariant("merged-readonly", "merged; signed in without write access", pullOpts{State: model.PRStateMerged, Who: whoReadonly}),
		prVariant("closed", "closed, with Reopen; writer", pullOpts{State: model.PRStateClosed, Who: whoReviewer}),
		prVariant("closed-readonly", "closed; signed in without write access", pullOpts{State: model.PRStateClosed, Who: whoReadonly}),
	)
	return out
}

func pullPages() []page {
	writer := pullOpts{State: model.PRStateOpen, Merge: mergeReadyState, Who: whoReviewer}
	readonly := pullOpts{State: model.PRStateOpen, Merge: mergeReadyState, Who: whoReadonly}
	return []page{
		{
			Name: "pulls", Route: "/{owner}/{repo}/pulls", Templ: "pulls.templ", Title: "Pull requests",
			Variants: []variant{
				{Note: "open tab", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return pages.Pulls(pullsData(b, "open")) }},
				{Name: "closed", Note: "closed tab", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return pages.Pulls(pullsData(b, "closed")) }},
				{Name: "no-match", Note: "a search that matches nothing", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Pulls(emptyPullsData(b, false, "cache invalidation"))
				}},
				{Name: "empty", Note: "no pull requests in any state", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Pulls(emptyPullsData(b, true, ""))
				}},
			},
		},
		{
			Name: "pr", Route: "/{owner}/{repo}/pulls/{n}", Templ: "pull_detail.templ", Title: "Pull request",
			Variants: prVariants(),
		},
		{
			Name: "pr_commits", Route: "/{owner}/{repo}/pulls/{n}/commits", Templ: "pr_commits.templ", Title: "Pull request commits",
			Variants: []variant{
				{Note: "three commits", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullCommits(pullCommitsData(b, writer))
				}},
				{Name: "empty", Note: "no commits between base and head", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullCommitsData(b, writer)
					d.Commits = nil
					return pages.PullCommits(d)
				}},
				{Name: "error", Note: "the commit list failed to load", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullCommitsData(b, writer)
					d.LoadError = true
					return pages.PullCommits(d)
				}},
			},
		},
		{
			Name: "pr_files", Route: "/{owner}/{repo}/pulls/{n}/files", Templ: "pr_files.templ", Title: "Pull request files changed",
			Variants: []variant{
				{Note: "modified, added, deleted and binary files with a line comment", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullFiles(pullFilesData(b, writer))
				}},
				{Name: "readonly", Note: "signed in without write access: no review or line-comment controls", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.PullFiles(pullFilesData(b, readonly))
				}},
				{Name: "empty", Note: "no changes", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullFilesData(b, writer)
					d.Diff, d.Tree = nil, nil
					return pages.PullFiles(d)
				}},
				{Name: "error", Note: "the diff failed to load", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullFilesData(b, writer)
					d.LoadError = true
					return pages.PullFiles(d)
				}},
			},
		},
		{
			Name: "pr_checks", Route: "/{owner}/{repo}/pulls/{n}/checks", Templ: "pr_checks.templ", Title: "Pull request checks",
			Variants: []variant{
				{Note: "one check failed", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullChecks(pullChecksData(b, writer, checkRows("success", "failure")))
				}},
				{Name: "passing", Note: "every check passed", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullChecks(pullChecksData(b, writer, checkRows("success", "success", "success")))
				}},
				{Name: "pending", Note: "checks still running", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullChecks(pullChecksData(b, writer, checkRows("success", "pending")))
				}},
				{Name: "empty", Note: "no status reported yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullChecks(pullChecksData(b, writer, nil))
				}},
				{Name: "error", Note: "statuses failed to load", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullChecksData(b, writer, nil)
					d.LoadError = true
					return pages.PullChecks(d)
				}},
			},
		},
		{
			Name: "actions", Route: "/{owner}/{repo}/checks", Templ: "checks.templ", Title: "Checks",
			Variants: []variant{
				{Note: "recent commits with statuses", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Checks(checksData(b, false))
				}},
				{Name: "empty", Note: "nothing reported yet, with the curl example", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Checks(checksData(b, true))
				}},
			},
		},
		{
			Name: "pull_new", Route: "/{owner}/{repo}/pulls/new", Templ: "pull_new.templ", Title: "Open a pull request",
			Variants: []variant{
				{Note: "writer picks reviewers and labels", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.PullNew(pullNewData(b, whoReviewer))
				}},
				{Name: "readonly", Note: "signed in without write access: reviewers and labels are read-only", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.PullNew(pullNewData(b, whoReadonly))
				}},
				{Name: "error", Note: "creating the pull request failed", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullNewData(b, whoReviewer)
					d.Error = "A pull request for these branches is already open."
					return pages.PullNew(d)
				}},
				{Name: "no-branches", Note: "the repository has no branches", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := pullNewData(b, whoReviewer)
					d.Branches = nil
					return pages.PullNew(d)
				}},
			},
		},
	}
}
