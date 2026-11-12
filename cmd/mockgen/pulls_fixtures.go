package main

import (
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/listquery"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

const (
	pullRepoName   = "hollow-atlas"
	pullHead       = "cache-tile-index"
	pullHeadSHA    = "9f3c1e07b2d84a5e6c0f1d2a3b4c5d6e7f809a1b"
	pullNumber     = 41
	pullOtherAuth  = "ada-silva"
	pullReviewer   = "nina-okafor"
	pullThirdParty = "aki-tanaka"
)

// pullWho is the viewer a pull request variant is drawn for.
type pullWho string

const (
	whoReviewer pullWho = "reviewer" // writer who did not open the PR
	whoAuthor   pullWho = "author"
	whoReadonly pullWho = "readonly" // signed in without write access
	whoAnon     pullWho = "anon"
)

func (w pullWho) viewer() Viewer {
	switch w {
	case whoReadonly:
		return Reader
	case whoAnon:
		return Anon
	}
	return Writer
}

func (w pullWho) canWrite() bool { return w == whoReviewer || w == whoAuthor }

// onPullRepo adds the repository chrome the pull request pages share.
func onPullRepo(b view.BasePage, active string, canWrite bool) view.BasePage {
	repo := fixtureRepos()[0]
	b.OwnerContext = fixtureUser
	b.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName: fixtureUser, RepoName: pullRepoName, Active: active, CanManage: canWrite,
		AllowIssues: repo.AllowIssues, AllowDiscussions: repo.AllowDiscussions,
		AllowProjects: repo.AllowProjects, AllowWiki: repo.AllowWiki,
		Counts: map[string]int{"pull_requests": 2, "issues": 7, "discussions": 3, "releases": 4},
	}
	for _, r := range fixtureRepos() {
		b.RepoSwitcher = append(b.RepoSwitcher, view.RepoRef{Name: r.Name, Path: "/" + fixtureUser + "/" + r.Name})
	}
	return b
}

func pullRepo() model.Repository { return fixtureRepos()[0] }

func pullPath(tail string) string {
	return "/" + fixtureUser + "/" + pullRepoName + tail
}

// pullBase builds the PR record. The author is the viewer for whoAuthor and someone else otherwise.
func pullBase(state model.PRState, draft bool, who pullWho) model.PullRequest {
	p := model.PullRequest{
		ID: 410, RepoID: 1, Number: pullNumber, AuthorID: 2, AuthorName: pullOtherAuth,
		Title: "Cache tile index on disk", State: state, IsDraft: draft,
		HeadBranch: pullHead, BaseBranch: "main", HeadSHA: pullHeadSHA,
		CreatedAt: ago(days(20)), UpdatedAt: ago(days(10)),
		Body: "Keeps the tile index on disk between restarts.",
	}
	if who == whoAuthor {
		p.AuthorID, p.AuthorName = 1, fixtureUser
	}
	switch state {
	case model.PRStateMerged:
		t := ago(days(12))
		p.MergedAt = &t
	case model.PRStateClosed:
		t := ago(days(12))
		p.ClosedAt = &t
	}
	return p
}

const pullBodyHTML = `<p>Rebuilding the tile index on every start takes minutes on a large fleet. This keeps it on disk and only rebuilds what changed.</p>
<ul>
<li>Persist the index next to the tile store</li>
<li>Invalidate by manifest hash</li>
<li>Fall back to a rebuild when the file is corrupt</li>
</ul>
<p>Reviewers: the invalidation path is the riskiest part.</p>`

func pullAuthorLogin(who pullWho) string {
	if who == whoAuthor {
		return fixtureUser
	}
	return pullOtherAuth
}

// pullMerge is one merge-box state of an open pull request.
type pullMerge string

const (
	mergeReadyState    pullMerge = "ready"
	mergeNeedsState    pullMerge = "needs"
	mergePendingState  pullMerge = "checks-pending"
	mergeFailingState  pullMerge = "failing"
	mergeChangesState  pullMerge = "changes"
	mergeConflictState pullMerge = "conflicts"
	mergeAutoState     pullMerge = "auto"
	mergeDraftState    pullMerge = "draft"
)

type pullOpts struct {
	State model.PRState
	Draft bool
	Merge pullMerge
	Who   pullWho
}

func pullMergeBox(o pullOpts) components.MergeabilityBoxData {
	d := components.MergeabilityBoxData{
		PatchURL:   "/api/repos/" + fixtureUser + "/" + pullRepoName + "/pulls/" + strconv.Itoa(pullNumber),
		BaseBranch: "main", HeadBranch: pullHead, CanWrite: o.Who.canWrite(),
		Mergeable: true, Ahead: 3, CanSquash: true, CanThreeWayMerge: true,
		RequiredChecks: 2, PassingChecks: 2, RequiredReviews: 1, ApprovedReviews: 1,
		ChecksURL: pullPath("/pulls/" + strconv.Itoa(pullNumber) + "/checks"),
	}
	switch o.Merge {
	case mergeNeedsState:
		d.ApprovedReviews = 0
	case mergePendingState:
		d.PassingChecks = 1
	case mergeFailingState:
		d.PassingChecks, d.FailingChecks = 1, 1
	case mergeChangesState:
		d.ApprovedReviews, d.ChangesRequested = 0, true
	case mergeConflictState:
		d.Mergeable, d.HasConflicts = false, true
	case mergeAutoState:
		d.ApprovedReviews, d.PassingChecks = 0, 1
		d.AutoMergeEnabled, d.AutoMergeStrategy, d.AutoMergeByName = true, "squash", pullReviewer
	case mergeDraftState:
		d.Draft, d.ApprovedReviews, d.PassingChecks = true, 0, 1
	}
	return d
}

func pullReviews(o pullOpts) []model.PullReview {
	submitted := ago(days(14))
	rev := func(id int64, name string, st model.PRReviewState, body string) model.PullReview {
		return model.PullReview{ID: id, PullID: 410, RepoID: 1, AuthorID: id + 10, AuthorName: name, State: st, Body: body,
			SubmittedAt: &submitted, CreatedAt: submitted, UpdatedAt: submitted}
	}
	switch o.Merge {
	case mergeNeedsState, mergeAutoState:
		return []model.PullReview{rev(1, pullReviewer, model.PRReviewPending, "")}
	case mergeChangesState:
		return []model.PullReview{rev(1, pullReviewer, model.PRReviewChangesRequested, "The invalidation path drops the manifest hash on a partial write.")}
	case mergeDraftState:
		return nil
	}
	return []model.PullReview{rev(1, pullReviewer, model.PRReviewApproved, "Looks good. One nit inline.")}
}

func pullChromeCounts(o pullOpts) view.PullChromeCounts {
	c := view.PullChromeCounts{ConvCount: 4, CommitsCount: 3, ChecksTotal: 2, ChecksPassed: 2, FilesCount: 5, Added: 64, Deleted: 17}
	switch o.Merge {
	case mergePendingState, mergeAutoState, mergeFailingState:
		c.ChecksPassed = 1
	}
	if o.State == model.PRStateMerged {
		c.MergedBy, c.MergedCommits = fixtureUser, 3
	}
	return c
}

func rendered(id int64, author, body string, at time.Time) view.RenderedComment {
	return view.RenderedComment{
		Comment:  model.Comment{ID: id, RepoID: 1, AuthorID: id + 10, AuthorName: author, Body: body, CreatedAt: at, UpdatedAt: at},
		BodyHTML: "<p>" + html.EscapeString(body) + "</p>",
	}
}

func pullEvents(o pullOpts) []model.PullEvent {
	ev := func(id int64, actor, typ, detail string, at time.Duration) model.PullEvent {
		return model.PullEvent{ID: id, PullID: 410, ActorName: actor, Type: typ, Detail: detail, CreatedAt: ago(at)}
	}
	out := []model.PullEvent{ev(1, pullOtherAuth, model.PullEventLabeled, "performance", days(19))}
	switch {
	case o.State == model.PRStateMerged:
		out = append(out, ev(2, fixtureUser, model.PullEventMerged, "3", days(12)))
	case o.State == model.PRStateClosed:
		out = append(out, ev(2, fixtureUser, model.PullEventClosed, "", days(12)))
	case o.Draft:
		out = append(out, ev(2, pullOtherAuth, model.PullEventDrafted, "", days(15)))
	}
	return out
}

func pullDetailData(b view.BasePage, o pullOpts) view.PullDetailData {
	repo := pullRepo()
	p := pullBase(o.State, o.Draft, o.Who)
	b = onPullRepo(b, "pull_requests", o.Who.canWrite())
	label := model.Label{ID: 1, RepoID: 1, Name: "performance", Color: "#1d76db"}
	ms := model.Milestone{ID: 1, RepoID: 1, Number: 2, Title: "v0.9 offline maps", State: "open", OpenCount: 4, ClosedCount: 6}
	d := view.PullDetailData{
		BasePage: b, PullChromeCounts: pullChromeCounts(o), Repo: repo, Pull: p,
		Owner: fixtureUser, RepoName: pullRepoName, AuthorUsername: pullAuthorLogin(o.Who), BodyHTML: pullBodyHTML,
		Labels: []model.Label{label}, AllLabels: []model.Label{label, {ID: 2, RepoID: 1, Name: "bug", Color: "#d73a4a"}},
		Assignees:     []model.User{{ID: 2, Username: pullOtherAuth}},
		Milestone:     &ms,
		AllMilestones: []model.Milestone{ms},
		CanWrite:      o.Who.canWrite(),
		Collaborators: []string{pullOtherAuth, pullReviewer, pullThirdParty},
		Reviews:       pullReviews(o),
		Comments: []view.RenderedComment{
			rendered(1, pullReviewer, "Does the index survive a partial write?", ago(days(16))),
			rendered(2, pullOtherAuth, "It writes to a temp file and renames, so a crash leaves the old index.", ago(days(15))),
		},
		Participants: []string{pullOtherAuth, pullReviewer},
		LinkedIssues: []view.LinkedIssue{{Number: 37, Title: "Slow cold start on large fleets", State: "open"}},
		LinkableIssues: []view.LinkedIssue{
			{Number: 37, Title: "Slow cold start on large fleets", State: "open"},
			{Number: 33, Title: "Tile index rebuilt on every restart", State: "open"},
		},
		ThreadSubscription: view.ThreadSubscriptionData{Owner: fixtureUser, RepoName: pullRepoName, Segment: "pulls", Number: pullNumber, State: "subscribed", Reason: "participating"},
		Events:             pullEvents(o),
		CanMerge:           true,
		Mergeability:       pullMergeBox(o),
	}
	if o.Merge == mergeAutoState {
		d.Pull.AutoMergeEnabled, d.Pull.AutoMergeStrategy = true, "squash"
		d.AutoMergeEnabled, d.AutoMergeStrategy = true, "squash"
	}
	return d
}

func pullCommits() []service.CommitSummary {
	return []service.CommitSummary{
		{Hash: "9f3c1e0", FullHash: pullHeadSHA, Message: "Invalidate the index by manifest hash\n\nA changed manifest forces a rebuild.", Author: pullOtherAuth, AuthorTime: ago(days(17))},
		{Hash: "4be7a21", FullHash: "4be7a21c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a", Message: "Persist the tile index next to the store", Author: pullOtherAuth, AuthorTime: ago(days(19))},
		{Hash: "c01d5f8", FullHash: "c01d5f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e", Message: "Add index benchmarks", Author: pullThirdParty, AuthorTime: ago(days(20))},
	}
}

func pullCommitsData(b view.BasePage, o pullOpts) view.PullCommitsData {
	repo, p := pullRepo(), pullBase(o.State, o.Draft, o.Who)
	return view.PullCommitsData{
		BasePage: onPullRepo(b, "pull_requests", o.Who.canWrite()), PullChromeCounts: pullChromeCounts(o),
		OwnerName: fixtureUser, Repo: &repo, Pull: &p, AuthorUsername: pullAuthorLogin(o.Who),
		Commits: pullCommits(), CanWrite: o.Who.canWrite(),
	}
}

func checkRows(states ...string) []components.CheckRow {
	names := []string{"ci/build", "ci/test", "ci/lint"}
	desc := map[string]string{"success": "Passed in 1m 12s", "failure": "2 tests failed", "pending": "Running", "error": "Runner lost"}
	rows := make([]components.CheckRow, 0, len(states))
	for i, s := range states {
		rows = append(rows, components.CheckRow{Context: names[i], State: s, Description: desc[s], URL: "https://ci.example.com/runs/" + strconv.Itoa(i+1)})
	}
	return rows
}

func pullChecksData(b view.BasePage, o pullOpts, rows []components.CheckRow) view.PullChecksData {
	repo, p := pullRepo(), pullBase(o.State, o.Draft, o.Who)
	c := pullChromeCounts(o)
	c.ChecksTotal, c.ChecksPassed = len(rows), 0
	for _, r := range rows {
		if r.State == "success" {
			c.ChecksPassed++
		}
	}
	return view.PullChecksData{
		BasePage: onPullRepo(b, "pull_requests", o.Who.canWrite()), PullChromeCounts: c,
		OwnerName: fixtureUser, Repo: &repo, Pull: &p, AuthorUsername: pullAuthorLogin(o.Who),
		Rows: rows, HeadSHA: pullHeadSHA, CanWrite: o.Who.canWrite(),
	}
}

func dl(typ string, old, new int, content string) service.DiffLine {
	return service.DiffLine{Type: typ, OldNum: old, NewNum: new, Content: content, HTML: template.HTML(html.EscapeString(content))}
}

func pullDiff() *service.PRDiffResult {
	idx := service.FileDiff{
		OldPath: "internal/tiles/index.go", NewPath: "internal/tiles/index.go", Added: 28, Deleted: 6,
		Hunks: []service.DiffHunk{{Header: "@@ -10,9 +10,14 @@ func (s *Store) loadIndex() error {", Lines: []service.DiffLine{
			dl("ctx", 10, 10, "func (s *Store) loadIndex() error {"),
			dl("del", 11, 0, "\tidx, err := s.rebuild()"),
			dl("add", 0, 11, "\tidx, err := s.readIndexFile()"),
			dl("add", 0, 12, "\tif errors.Is(err, errIndexStale) {"),
			dl("add", 0, 13, "\t\tidx, err = s.rebuild()"),
			dl("add", 0, 14, "\t}"),
			dl("ctx", 12, 15, "\tif err != nil {"),
			dl("ctx", 13, 16, "\t\treturn err"),
			dl("ctx", 14, 17, "\t}"),
			dl("del", 15, 0, "\ts.index = idx"),
			dl("add", 0, 18, "\ts.index = idx // swapped under mu"),
		}}},
	}
	test := service.FileDiff{
		NewPath: "internal/tiles/index_test.go", IsNew: true, Added: 3,
		Hunks: []service.DiffHunk{{Header: "@@ -0,0 +1,3 @@", Lines: []service.DiffLine{
			dl("add", 0, 1, "package tiles"),
			dl("add", 0, 2, ""),
			dl("add", 0, 3, "func TestIndexRoundTrip(t *testing.T) {}"),
		}}},
	}
	legacy := service.FileDiff{
		OldPath: "internal/tiles/legacy.go", IsDelete: true, Deleted: 2,
		Hunks: []service.DiffHunk{{Header: "@@ -1,2 +0,0 @@", Lines: []service.DiffLine{
			dl("del", 1, 0, "package tiles"),
			dl("del", 2, 0, "// Deprecated: use Store."),
		}}},
	}
	doc := service.FileDiff{
		OldPath: "docs/caching.md", NewPath: "docs/caching.md", Added: 2, Deleted: 1,
		Hunks: []service.DiffHunk{{Header: "@@ -4,3 +4,4 @@ ## Index", Lines: []service.DiffLine{
			dl("ctx", 4, 4, "The tile index is kept in memory."),
			dl("del", 5, 0, "It is rebuilt on every start."),
			dl("add", 0, 5, "It is persisted next to the tile store"),
			dl("add", 0, 6, "and rebuilt when the manifest changes."),
		}}},
	}
	bin := service.FileDiff{OldPath: "assets/legend.png", NewPath: "assets/legend.png", IsBinary: true}
	files := []service.FileDiff{idx, test, legacy, doc, bin}
	return &service.PRDiffResult{Files: files, TotalAdded: 64, TotalDeleted: 17}
}

func pullFilesData(b view.BasePage, o pullOpts) view.PullFilesData {
	repo, p := pullRepo(), pullBase(o.State, o.Draft, o.Who)
	diff := pullDiff()
	tree := make([]components.DiffFileTreeItem, 0, len(diff.Files))
	for i, f := range diff.Files {
		tree = append(tree, components.DiffFileTreeItem{Path: f.DisplayPath(), Anchor: "diff-" + strconv.Itoa(i), Added: f.Added, Deleted: f.Deleted})
	}
	at := ago(days(15))
	key := view.LineCommentKey{Side: "right", Path: "internal/tiles/index.go", Line: 18}
	return view.PullFilesData{
		BasePage: onPullRepo(b, "pull_requests", o.Who.canWrite()), PullChromeCounts: pullChromeCounts(o),
		OwnerName: fixtureUser, Repo: &repo, Pull: &p, AuthorUsername: pullAuthorLogin(o.Who),
		Tree: tree, Diff: diff, CanWrite: o.Who.canWrite(),
		LineComments: map[view.LineCommentKey][]view.RenderedLineComment{key: {{
			PullLineComment: model.PullLineComment{
				ID: 1, PullID: 410, RepoID: 1, AuthorID: 12, AuthorName: pullReviewer, Path: key.Path, DiffSide: key.Side,
				Line: key.Line, Body: "Say which lock guards this.", CreatedAt: at, UpdatedAt: at,
			},
			BodyHTML: "<p>Say which lock guards this.</p>",
		}}},
	}
}

func pullNewData(b view.BasePage, who pullWho) view.PullNewData {
	return view.PullNewData{
		BasePage: onPullRepo(b, "pull_requests", who.canWrite()), Repo: pullRepo(), Owner: fixtureUser, RepoName: pullRepoName,
		Branches: []service.BranchInfo{{Name: pullHead, Hash: "9f3c1e0"}, {Name: "main", Hash: "2a9d4b6", IsDefault: true}, {Name: "docs-refresh", Hash: "71ce0aa"}},
		Base:     "main", Head: pullHead, CanWrite: who.canWrite(),
		AllLabels: []model.Label{{ID: 1, Name: "performance", Color: "#1d76db"}, {ID: 2, Name: "bug", Color: "#d73a4a"}},
		Reviewer: components.ReviewerPickerData{
			Suggested: []components.ReviewerOption{{Username: pullReviewer, Reason: "CODEOWNERS"}},
			All:       []components.ReviewerOption{{Username: pullOtherAuth}, {Username: pullReviewer}, {Username: pullThirdParty}},
		},
	}
}

func listOpt(key, value, label string, checked bool) components.ListFilterOption {
	return components.ListFilterOption{Token: listquery.Token(key, value), Label: label, Checked: checked}
}

func pullFilter(state string, total int, text string) components.ListFilterData {
	base := pullPath("/pulls")
	people := []string{
		listquery.Me, pullOtherAuth, pullReviewer, pullThirdParty, "amara-silva", "anika-jansen", "beatriz-fischer",
		"ines-novak", "sofia-haddad", "wen-yilmaz",
	}
	author := make([]components.ListFilterOption, 0, len(people))
	for _, n := range people {
		author = append(author, listOpt(listquery.KeyAuthor, n, n, false))
	}
	sort := []components.ListFilterOption{
		listOpt(listquery.KeySort, "newest", "Newest", true), listOpt(listquery.KeySort, "oldest", "Oldest", false),
		listOpt(listquery.KeySort, "updated", "Recently updated", false), listOpt(listquery.KeySort, "comments", "Most commented", false),
	}
	review := []components.ListFilterOption{
		listOpt(listquery.KeyReview, "required", "Review required", false), listOpt(listquery.KeyReview, "approved", "Approved", false),
		listOpt(listquery.KeyReview, "changes_requested", "Changes requested", false), listOpt(listquery.KeyReview, "none", "No reviews", false),
	}
	label := []components.ListFilterOption{
		{Token: listquery.Token(listquery.KeyLabel, "performance"), Label: "performance", Color: "#1d76db"},
		{Token: listquery.Token(listquery.KeyLabel, "bug"), Label: "bug", Color: "#d73a4a"},
		{Token: listquery.Token(listquery.KeyNo, "label"), Label: "No label", Divider: true},
	}
	d := components.ListFilterData{
		Action: base, Placeholder: "Search or filter, e.g. is:open label:bug author:@me", Text: text,
		SearchBase: "is:" + state, ClearHref: base + "?q=is%3A" + state, HasFilters: text != "", Total: total, Noun: "pull request",
		Menus: []components.ListFilterMenu{
			{Title: "Author", Base: "is:" + state, Options: author},
			{Title: "Label", Multi: true, Base: "is:" + state, Options: label},
			{Title: "Milestone", Base: "is:" + state, Options: []components.ListFilterOption{listOpt(listquery.KeyMilestone, "v0.9 offline maps", "v0.9 offline maps", false)}},
			{Title: "Assignee", Base: "is:" + state, Options: author},
			{Title: "Review", Base: "is:" + state, Options: review},
			{Title: "Sort", Base: "is:" + state, Options: sort},
		},
	}
	return d
}

func pullRow(n int, title, author, state, ci string, passing, total, comments int, review string) components.PRListRowData {
	return components.PRListRowData{
		OwnerName: fixtureUser, RepoName: pullRepoName, Number: n, Title: title, Author: author, State: state,
		CIStatus: ci, CIPassing: passing, CITotal: total, CommentCount: comments, ReviewState: review,
		OpenedAt: "opened on Jun 2, 2025",
	}
}

func withMeta(r components.PRListRowData, labels []components.LabelChipData, reviewers ...string) components.PRListRowData {
	r.LabelChips, r.ReviewerAvatars = labels, reviewers
	return r
}

func pullsData(b view.BasePage, state string) view.PullsData {
	d := view.PullsData{
		BasePage: onPullRepo(b, "pull_requests", true), Repo: pullRepo(), Owner: fixtureUser, RepoName: pullRepoName,
		StateFilter: state, OpenURL: pullPath("/pulls"), DraftURL: pullPath("/pulls?q=is%3Adraft"),
		MergedURL: pullPath("/pulls?q=is%3Amerged"), ClosedURL: pullPath("/pulls?q=is%3Aclosed"),
		ClearURL: pullPath("/pulls"), Page: 1, TotalPages: 1,
		OpenCount: 2, DraftCount: 1, MergedCount: 5, ClosedCount: 2,
	}
	switch state {
	case "closed":
		d.Rows = []components.PRListRowData{
			pullRow(36, "Drop the legacy tile format", pullThirdParty, "closed", "", 0, 0, 2, ""),
			pullRow(29, "Try an LRU for the tile index", pullOtherAuth, "closed", "failure", 1, 2, 5, "changes_requested"),
		}
		d.Filter = pullFilter(state, 2, "")
	default:
		d.Rows = []components.PRListRowData{
			withMeta(pullRow(41, "Cache tile index on disk", pullOtherAuth, "open", "success", 2, 2, 4, "approved"),
				[]components.LabelChipData{{Name: "performance", Color: "#1d76db"}}, "NO", "AT"),
			pullRow(40, "Retry failed tile fetches with backoff", pullThirdParty, "open", "pending", 1, 2, 1, "required"),
		}
		d.Filter = pullFilter(state, 2, "")
	}
	return d
}

func emptyPullsData(b view.BasePage, repoEmpty bool, text string) view.PullsData {
	d := pullsData(b, "open")
	d.Rows = nil
	d.RepoEmpty = repoEmpty
	d.OpenCount, d.DraftCount, d.MergedCount, d.ClosedCount = 0, 0, 0, 0
	d.Filter = pullFilter("open", 0, text)
	return d
}

func checkedAt(sha string, subject string, state model.CommitStatusState, at time.Duration, statuses ...model.CommitStatus) service.CommitChecks {
	c := service.CommitChecks{SHA: sha, Subject: subject, State: state, UpdatedAt: ago(at), Statuses: statuses}
	for _, s := range statuses {
		if s.State == model.CommitStatusSuccess {
			c.Passed++
		}
	}
	return c
}

func status(ctx string, st model.CommitStatusState, desc string) model.CommitStatus {
	return model.CommitStatus{Context: ctx, State: st, Description: desc, TargetURL: "https://ci.example.com/runs/" + strings.ReplaceAll(ctx, "/", "-")}
}

func checksData(b view.BasePage, empty bool) view.ChecksData {
	d := view.ChecksData{
		BasePage: onPullRepo(b, "checks", true), Repo: pullRepo(), Owner: fixtureUser, RepoName: pullRepoName,
		StatusAPIURL: "http://localhost:8080/api/repos/" + fixtureUser + "/" + pullRepoName + "/statuses/<sha>",
	}
	if empty {
		return d
	}
	d.OlderURL = pullPath("/checks?page=2")
	d.Commits = []service.CommitChecks{
		checkedAt(pullHeadSHA, "Invalidate the index by manifest hash", model.CommitStatusFailure, days(10),
			status("ci/build", model.CommitStatusSuccess, "Passed in 1m 12s"), status("ci/test", model.CommitStatusFailure, "2 tests failed")),
		checkedAt("4be7a21c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a", "Persist the tile index next to the store", model.CommitStatusSuccess, days(12),
			status("ci/build", model.CommitStatusSuccess, "Passed in 1m 05s"), status("ci/test", model.CommitStatusSuccess, "Passed in 2m 30s")),
		checkedAt("c01d5f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e", "Add index benchmarks", model.CommitStatusPending, days(14),
			status("ci/build", model.CommitStatusPending, "Running")),
	}
	return d
}
