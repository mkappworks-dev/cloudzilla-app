package main

import (
	"database/sql"
	"net/url"
	"regexp"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/listquery"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// Fixtures for the issue, milestone and discussion pages: one repository, shared by every variant.
const (
	trackerRepoName = "hollow-atlas"
	trackerBasePath = "/" + fixtureUser + "/" + trackerRepoName
)

// access is what the viewer can do in the repository; the handlers derive it from the viewer's role.
type access struct{ canWrite, canManage bool }

func accessFor(v Viewer) access {
	return access{canWrite: v == Writer || v == Admin, canManage: v == Writer || v == Admin}
}

func trackerRepo() model.Repository { return fixtureRepos()[0] }

// onTrackerRepo adds the repository chrome (tab bar and repo switcher) the handlers add with withRepoSubnav.
func onTrackerRepo(b view.BasePage, active string, a access) view.BasePage {
	repo := trackerRepo()
	b.OwnerContext = fixtureUser
	b.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName: fixtureUser, RepoName: repo.Name, Active: active, CanManage: a.canManage,
		AllowIssues: true, AllowDiscussions: true, AllowProjects: true, AllowWiki: true,
		Counts: map[string]int{"pull_requests": 2, "issues": 4, "discussions": 3, "releases": 1},
	}
	for _, r := range fixtureRepos() {
		b.RepoSwitcher = append(b.RepoSwitcher, view.RepoRef{Name: r.Name, Path: "/" + r.OwnerName + "/" + r.Name})
	}
	return b
}

var (
	labelBug     = model.Label{ID: 1, RepoID: 1, Name: "bug", Color: "#d73a4a", Description: "Something isn't working"}
	labelEnh     = model.Label{ID: 2, RepoID: 1, Name: "enhancement", Color: "#0e8a16", Description: "New feature or request"}
	labelDocs    = model.Label{ID: 3, RepoID: 1, Name: "documentation", Color: "#0075ca", Description: "Improvements to the docs"}
	labelStarter = model.Label{ID: 4, RepoID: 1, Name: "good first issue", Color: "#7057ff", Description: "Good for newcomers"}
	trackerLabel = []model.Label{labelBug, labelEnh, labelDocs, labelStarter}
)

func priority(p string) *string { return &p }

func int64Ptr(n int64) *int64 { return &n }

func trackerMilestones() []model.Milestone {
	due := ago(-days(21))
	closedAt := ago(days(20))
	return []model.Milestone{
		{ID: 1, RepoID: 1, Number: 1, Title: "v1.0 — offline tiles", State: "open", DueDate: &due,
			Description: "Ship the tile cache and the fleet sync agent.", CreatedAt: ago(days(90)), UpdatedAt: ago(days(9)),
			OpenCount: 3, ClosedCount: 5},
		{ID: 2, RepoID: 1, Number: 2, Title: "Docs pass", State: "open",
			Description: "Fill the gaps in the operator guide.", CreatedAt: ago(days(60)), UpdatedAt: ago(days(14)),
			OpenCount: 1, ClosedCount: 0},
		{ID: 3, RepoID: 1, Number: 3, Title: "v0.9 — alpha", State: "closed", ClosedAt: &closedAt,
			Description: "First tagged build.", CreatedAt: ago(days(150)), UpdatedAt: ago(days(20)),
			OpenCount: 0, ClosedCount: 7},
	}
}

func trackerIssues() []model.Issue {
	mk := func(id int64, n int, title, author string, state model.IssueState, age time.Duration) model.Issue {
		return model.Issue{ID: id, RepoID: 1, Number: n, AuthorName: author, Title: title, State: state, Visibility: "public",
			CreatedAt: ago(age), UpdatedAt: ago(age)}
	}
	a := mk(101, 57, "Tile index grows without bound on long-running agents", "ada-silva", model.IssueStateOpen, days(11))
	a.Priority = priority("high")
	b := mk(102, 55, "Add a --dry-run flag to the sync command", "tomas-lindqvist", model.IssueStateOpen, days(14))
	c := mk(103, 52, "Document the S3 backend credentials", fixtureUser, model.IssueStateOpen, days(18))
	c.Priority = priority("low")
	d := mk(104, 48, "Sync stalls when the fleet manifest is empty", "ada-silva", model.IssueStateOpen, days(25))
	return []model.Issue{a, b, c, d}
}

func trackerClosedIssues() []model.Issue {
	mk := func(id int64, n int, title, author string, age time.Duration) model.Issue {
		closed := ago(age - days(2))
		return model.Issue{ID: id, RepoID: 1, Number: n, AuthorName: author, Title: title, State: model.IssueStateClosed,
			Visibility: "public", CreatedAt: ago(age), UpdatedAt: closed, ClosedAt: &closed}
	}
	return []model.Issue{
		mk(110, 44, "Crash on startup with a read-only data directory", "tomas-lindqvist", days(40)),
		mk(111, 41, "Typo in the install guide", fixtureUser, days(46)),
		mk(112, 39, "Tile checksums are not verified after download", "ada-silva", days(52)),
	}
}

func trackerIssueLabels() map[int64][]model.Label {
	return map[int64][]model.Label{101: {labelBug}, 102: {labelEnh, labelStarter}, 103: {labelDocs}, 104: {labelBug},
		110: {labelBug}, 111: {labelDocs}, 112: {labelBug}}
}

func trackerCommentCounts() map[int64]int {
	return map[int64]int{101: 4, 102: 1, 104: 2, 110: 3, 112: 6}
}

// issueFilter mirrors the handler's listFilterData for issues (menus, chips, notices); the handler's copy is unexported.
// raw is the q= value; total is the number of matches.
func issueFilter(raw string, total int, signedIn bool) components.ListFilterData {
	q := listquery.Parse(raw)
	link := func(q listquery.Query) string {
		if s := q.String(); s != "" {
			return trackerBasePath + "/issues?q=" + url.QueryEscape(s)
		}
		return trackerBasePath + "/issues"
	}
	d := components.ListFilterData{
		Action: trackerBasePath + "/issues", Placeholder: "Search or filter, e.g. is:open label:bug author:@me",
		Text: q.Text, SearchBase: q.Without(listquery.KeyText, "").String(), ClearHref: link(listquery.Query{State: q.State}),
		HasFilters: q.HasFilters(), Total: total, Noun: "issue", Notices: q.Unrecognized(),
	}
	add := func(key, value string) {
		d.Chips = append(d.Chips, components.ListFilterChip{Label: listquery.Token(key, value), RemoveHref: link(q.Without(key, value))})
	}
	if q.Author != "" {
		add(listquery.KeyAuthor, q.Author)
	}
	if q.Assignee != "" {
		add(listquery.KeyAssignee, q.Assignee)
	}
	for _, l := range q.Labels {
		add(listquery.KeyLabel, l)
	}
	if q.Milestone != "" {
		add(listquery.KeyMilestone, q.Milestone)
	}
	for _, n := range q.No {
		add(listquery.KeyNo, n)
	}
	if q.Sort != "" {
		add(listquery.KeySort, q.Sort)
	}

	menu := func(title string, multi bool, strip func(listquery.Query) listquery.Query, opts []components.ListFilterOption) {
		m := components.ListFilterMenu{Title: title, Multi: multi, Base: strip(q).String(), Options: opts}
		if multi {
			for _, o := range opts {
				if o.Checked {
					m.Selected++
				}
			}
		}
		d.Menus = append(d.Menus, m)
	}
	opt := func(key, value, label string) components.ListFilterOption {
		return components.ListFilterOption{Token: listquery.Token(key, value), Label: label, Checked: q.Has(key, value)}
	}
	none := func(key, label string, divider bool) components.ListFilterOption {
		o := opt(listquery.KeyNo, key, label)
		o.Divider = divider
		return o
	}
	names := trackerPeople()
	if signedIn {
		names = append([]string{listquery.Me}, names...)
	}
	person := func(key string) []components.ListFilterOption {
		opts := make([]components.ListFilterOption, 0, len(names)+1)
		for _, n := range names {
			opts = append(opts, opt(key, n, n))
		}
		return opts
	}

	menu("Author", false, func(q listquery.Query) listquery.Query { return q.Without(listquery.KeyAuthor, "") },
		person(listquery.KeyAuthor))

	var labelOpts []components.ListFilterOption
	for _, l := range trackerLabel {
		o := opt(listquery.KeyLabel, l.Name, l.Name)
		o.Color = l.Color
		labelOpts = append(labelOpts, o)
	}
	labelOpts = append(labelOpts, none("label", "No label", true))
	menu("Label", true, func(q listquery.Query) listquery.Query {
		for _, l := range q.Labels {
			q = q.Without(listquery.KeyLabel, l)
		}
		return q.Without(listquery.KeyNo, "label")
	}, labelOpts)

	var msOpts []components.ListFilterOption
	for _, m := range trackerMilestones() {
		msOpts = append(msOpts, opt(listquery.KeyMilestone, m.Title, m.Title))
	}
	msOpts = append(msOpts, none("milestone", "No milestone", true))
	menu("Milestone", false, func(q listquery.Query) listquery.Query {
		return q.Without(listquery.KeyMilestone, "").Without(listquery.KeyNo, "milestone")
	}, msOpts)

	menu("Assignee", false, func(q listquery.Query) listquery.Query {
		return q.Without(listquery.KeyAssignee, "").Without(listquery.KeyNo, "assignee")
	}, append(person(listquery.KeyAssignee), none("assignee", "Assigned to nobody", true)))

	var sortOpts []components.ListFilterOption
	for _, s := range []struct{ value, label string }{
		{"newest", "Newest"}, {"oldest", "Oldest"}, {"updated", "Recently updated"}, {"comments", "Most commented"},
	} {
		o := opt(listquery.KeySort, s.value, s.label)
		o.Checked = q.Sort == s.value || q.Sort == "" && s.value == "newest"
		sortOpts = append(sortOpts, o)
	}
	menu("Sort", false, func(q listquery.Query) listquery.Query { return q.Without(listquery.KeySort, "") }, sortOpts)
	return d
}

// openFilterMenu opens the filter menu titled title, which the template renders closed; a <details> has no state the
// fixtures can set.
func openFilterMenu(title string) func([]byte) []byte {
	re := regexp.MustCompile(`(?s)<details(\s[^>]*?)>(\s*<summary[^>]*>\s*` + regexp.QuoteMeta(title) + `\s)`)
	return func(html []byte) []byte { return re.ReplaceAll(html, []byte("<details open$1>$2")) }
}

// trackerPeople has more than eight names, which is when the real menu adds its own search field.
func trackerPeople() []string {
	return []string{"ada-silva", "beatriz-fischer", "dee-park", "felix-eriksen", fixtureUser, "sofia-haddad", "tomas-lindqvist",
		"wen-yilmaz", "zoe-okafor"}
}

func trackerCollaborators() []string { return []string{"ada-silva", fixtureUser, "tomas-lindqvist"} }

func trackerAssignees() []model.User {
	return []model.User{{ID: 2, Username: "ada-silva", Name: "Ada Silva"}}
}

func trackerPulls() []model.PullRequest {
	merged := ago(days(8))
	return []model.PullRequest{
		{ID: 201, RepoID: 1, Number: 58, AuthorName: "ada-silva", Title: "Evict stale tile index entries", State: model.PRStateOpen,
			HeadBranch: "fix/tile-index-eviction", BaseBranch: "main", CreatedAt: ago(days(10)), UpdatedAt: ago(days(9))},
		{ID: 202, RepoID: 1, Number: 54, AuthorName: "tomas-lindqvist", Title: "Verify tile checksums on download", State: model.PRStateMerged,
			HeadBranch: "feat/verify-checksums", BaseBranch: "main", CreatedAt: ago(days(16)), UpdatedAt: merged, MergedAt: &merged},
	}
}

func trackerComments() []view.RenderedComment {
	mk := func(id int64, author, body string, age time.Duration) view.RenderedComment {
		return view.RenderedComment{
			Comment:  model.Comment{ID: id, RepoID: 1, AuthorName: author, Body: body, CreatedAt: ago(age), UpdatedAt: ago(age)},
			BodyHTML: "<p>" + body + "</p>",
		}
	}
	return []view.RenderedComment{
		mk(301, "tomas-lindqvist", "I can reproduce this on a 3-node fleet after about a week of uptime.", days(10)),
		mk(302, fixtureUser, "Thanks. The eviction pass only runs on startup, so a long-lived agent never trims the index.", days(10)),
		mk(303, "ada-silva", "I have a fix in #58, which evicts entries older than the retention window.", days(9)),
	}
}

var (
	catQA    = model.DiscussionCategory{ID: 1, Name: "Q&A", Description: "Ask the maintainers and the community"}
	catIdeas = model.DiscussionCategory{ID: 2, Name: "Ideas", Description: "Share ideas for new features"}
	catShow  = model.DiscussionCategory{ID: 3, Name: "Show and tell", Description: "Show what you built with it"}
)

func trackerCategories() []model.DiscussionCategory {
	return []model.DiscussionCategory{catQA, catIdeas, catShow}
}

func trackerDiscussions() []model.Discussion {
	mk := func(id int64, n int, cat int64, title, author string, age time.Duration) model.Discussion {
		return model.Discussion{ID: id, RepoID: 1, CategoryID: cat, Number: n, Title: title, Body: title, AuthorName: author,
			CreatedAt: ago(age), UpdatedAt: ago(age)}
	}
	open1 := mk(401, 12, 1, "How do I run the sync agent behind a proxy?", "tomas-lindqvist", days(12))
	open2 := mk(402, 11, 2, "Support vector tiles as well as raster?", "ada-silva", days(20))
	answered := mk(403, 9, 1, "Where does the tile cache live on Windows?", "ada-silva", days(30))
	answered.IsAnswered = true
	answered.AnswerID = sql.NullInt64{Int64: 502, Valid: true}
	locked := mk(404, 7, 3, "Our fleet of 400 field tablets", "tomas-lindqvist", days(60))
	locked.IsLocked = true
	return []model.Discussion{open1, open2, answered, locked}
}

func trackerDiscussionsData(b view.BasePage, a access, state string, active int64) view.DiscussionsData {
	all := trackerDiscussions()
	d := view.DiscussionsData{
		BasePage: onTrackerRepo(b, "discussions", a), Repo: trackerRepo(), Owner: fixtureUser, RepoName: trackerRepoName,
		Categories: trackerCategories(), ActiveCategoryID: active, StateFilter: state, CanWrite: a.canWrite,
		CategoryCounts: map[int64]int{}, Labels: map[int64][]model.Label{402: {labelEnh}},
		ReplyCounts:  map[int64]int{401: 2, 402: 5, 403: 3, 404: 8},
		Participants: map[int64][]string{401: {"tomas-lindqvist", fixtureUser}, 402: {"ada-silva", fixtureUser, "tomas-lindqvist", "dee-park"}, 403: {"ada-silva"}, 404: {"tomas-lindqvist"}},
	}
	for _, x := range all {
		d.TotalCount++
		d.CategoryCounts[x.CategoryID]++
		switch {
		case x.IsAnswered:
			d.AnsweredCount++
		case x.IsLocked:
			d.ClosedCount++
		default:
			d.OpenCount++
		}
		if active != 0 && x.CategoryID != active {
			continue
		}
		inTab := map[string]bool{
			"answered": x.IsAnswered,
			"closed":   x.IsLocked && !x.IsAnswered,
			"open":     !x.IsAnswered && !x.IsLocked,
		}[state]
		if inTab {
			d.Discussions = append(d.Discussions, x)
		}
	}
	return d
}
