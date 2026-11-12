package handler

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/mkappworks-dev/cloudzilla-app/internal/listquery"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// accountScope is a tab of an account list: it sets one @me qualifier. legacy is the old ?filter= value, which also keys
// the tab counts.
type accountScope struct {
	key, label, legacy string
}

var (
	// The first scope is the default for a bare URL, as the first tab was before the qualifier bar.
	accountIssueScopes = []accountScope{
		{listquery.KeyAssignee, "Assigned", "assigned"},
		{listquery.KeyAuthor, "Created by you", "created"},
		{listquery.KeyMentions, "Mentioned", "mentioned"},
	}
	accountPullScopes = []accountScope{
		{listquery.KeyAuthor, "Created by you", "created"},
		{listquery.KeyAssignee, "Assigned", "assigned"},
		{listquery.KeyReviewer, "Review requests", "review_requested"},
		{listquery.KeyMentions, "Mentioned", "mentioned"},
	}
)

// withScope returns q showing only the scope's @me qualifier, so the tabs switch rather than stack.
func withScope(q listquery.Query, scopes []accountScope, key string) listquery.Query {
	for _, sc := range scopes {
		if q.Has(sc.key, listquery.Me) {
			q = q.Without(sc.key, "")
		}
	}
	return q.With(key, listquery.Me)
}

// redirectAccountList sends the old ?filter=, ?state= and ?sort= URLs, and a bare address, to the canonical ?q= one, so
// every account list view has the address the qualifier bar would build.
func redirectAccountList(w http.ResponseWriter, r *http.Request, base string, scopes []accountScope) bool {
	if redirectListForm(w, r, base) {
		return true
	}
	qv := r.URL.Query()
	legacy := qv.Has("filter") || qv.Has("state") || qv.Has("sort")
	if qv.Has("q") && !legacy {
		return false
	}
	q := listquery.FromValues(qv)
	if q.State == "open" {
		q = q.Without(listquery.KeyIs, "")
	}
	// An address with a q and no filter= keeps the scope its q gives, even none.
	if !qv.Has("q") || qv.Has("filter") {
		key := scopes[0].key
		for _, sc := range scopes {
			if sc.legacy == qv.Get("filter") {
				key = sc.key
			}
		}
		q = withScope(q, scopes, key)
	}
	http.Redirect(w, r, listURL(base, q, 1), http.StatusSeeOther)
	return true
}

// accountStoreFilter is listStoreFilter plus the repository, which only the account lists can filter on.
func accountStoreFilter(viewer *middleware.Claims, q listquery.Query, state string, page int) store.ListFilter {
	f := listStoreFilter(viewer, q, state, page)
	f.Repo = q.Repo
	return f
}

// accountListNav builds the tabs, search bar and paging around an account list. repos are the "owner/name" choices of
// the repository menu.
func accountListNav(base, noun string, scopes []accountScope, withReview bool, q listquery.Query, state string, counts map[string]int, total, page int, repos []string) view.AccountListNav {
	link := func(q listquery.Query) string { return listURL(base, q, 1) }
	countState := "open"
	if state == "closed" {
		countState = "closed"
	}
	nav := view.AccountListNav{
		Filter:      accountFilterData(base, noun, withReview, q, total, repos),
		StateFilter: state,
		OpenURL:     link(q.Without(listquery.KeyIs, "")),
		ClosedURL:   link(q.With(listquery.KeyIs, "closed")),
		ClearURL:    link(listquery.Query{State: q.State}),
		Counts:      counts,
		Page:        page,
		TotalPages:  listTotalPages(total),
	}
	for _, sc := range scopes {
		active := q.Has(sc.key, listquery.Me)
		nav.ScopeTabs = append(nav.ScopeTabs, view.AccountListTab{
			Label: sc.label, URL: link(withScope(q, scopes, sc.key)), Active: active, CountKey: sc.legacy + ":" + countState,
		})
		if active && nav.ActiveScope == "" {
			nav.ActiveScope = sc.legacy
		}
	}
	if page > 1 {
		nav.PrevURL = listURL(base, q, page-1)
	}
	if page < nav.TotalPages {
		nav.NextURL = listURL(base, q, page+1)
	}
	return nav
}

// accountFilterData is the repository lists' search bar without the menus that belong to one repository (labels and
// milestones), plus a Repository menu.
func accountFilterData(base, noun string, withReview bool, q listquery.Query, total int, repos []string) components.ListFilterData {
	d := listFilterData(base, noun, withReview, q, total, nil, true, nil, nil)
	d.Placeholder = "Search or filter, e.g. repo:owner/name author:@me"
	link := func(q listquery.Query) string { return listURL(base, q, 1) }

	repoMenu := components.ListFilterMenu{Title: "Repository", Base: q.Without(listquery.KeyRepo, "").String()}
	if q.Repo != "" && !slices.ContainsFunc(repos, func(r string) bool { return q.Has(listquery.KeyRepo, r) }) {
		repos = append([]string{q.Repo}, repos...)
	}
	for _, name := range repos {
		repoMenu.Options = append(repoMenu.Options, components.ListFilterOption{
			Token: listquery.Token(listquery.KeyRepo, name), Label: name, Checked: q.Has(listquery.KeyRepo, name),
		})
	}

	menus := d.Menus[:0:0]
	for _, m := range d.Menus {
		switch m.Title {
		case "Label", "Milestone":
			continue
		case "Review":
			// "Review required" depends on each repository's branch rules, which a cross-repository list doesn't load.
			m.Options = slices.DeleteFunc(slices.Clone(m.Options), func(o components.ListFilterOption) bool {
				return o.Token == listquery.Token(listquery.KeyReview, "required")
			})
		case "Sort":
			if len(repoMenu.Options) > 0 {
				menus = append(menus, repoMenu)
			}
		}
		menus = append(menus, m)
	}
	d.Menus = menus

	// The tabs show open and closed, so only the other states need a chip to be seen and removed.
	if q.State != "" && q.State != "closed" {
		chip := components.ListFilterChip{Label: listquery.Token(listquery.KeyIs, q.State), RemoveHref: link(q.Without(listquery.KeyIs, ""))}
		d.Chips = append([]components.ListFilterChip{chip}, d.Chips...)
	}
	return d
}

// accountListQuery is the request's query with what the list can't honour taken out, and a notice for each.
func accountListQuery(v url.Values, issues bool) (listquery.Query, []string) {
	q := listquery.FromValues(v)
	var notes []string
	switch {
	case issues && (q.Review != "" || q.Reviewer != ""):
		q = q.Without(listquery.KeyReview, "").Without(listquery.KeyReviewer, "")
		notes = append(notes, "review: and reviewer: apply to pull requests, so they were ignored.")
	case !issues && q.Review == "required":
		q = q.Without(listquery.KeyReview, "")
		notes = append(notes, "review:required depends on each repository’s branch rules, so it was ignored here.")
	}
	return q, notes
}
