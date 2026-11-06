package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/listquery"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

const listPageSize = 25

// redirectListForm turns a submitted search or menu form into the canonical ?q= URL, so a filtered list always has one
// shareable address. The forms send f (the query minus what the control sets), v (menu qualifier tokens) and q (typed
// text, which may itself hold qualifiers).
func redirectListForm(w http.ResponseWriter, r *http.Request) bool {
	qv := r.URL.Query()
	if !qv.Has("f") && !qv.Has("v") {
		return false
	}
	parts := append([]string{qv.Get("f")}, qv["v"]...)
	parts = append(parts, qv.Get("q"))
	http.Redirect(w, r, listURL(r.URL.Path, listquery.Parse(strings.Join(parts, " ")), 1), http.StatusSeeOther)
	return true
}

// listURL is the address of page of the list at base showing q.
func listURL(base string, q listquery.Query, page int) string {
	v := url.Values{}
	if s := q.String(); s != "" {
		v.Set("q", s)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return base
	}
	return base + "?" + v.Encode()
}

func listPage(r *http.Request) int {
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		return p
	}
	return 1
}

func listTotalPages(total int) int {
	return max(1, (total+listPageSize-1)/listPageSize)
}

// listViewer is the signed-in user, or nil.
func listViewer(r *http.Request) *middleware.Claims {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		return &claims
	}
	return nil
}

// listStoreFilter converts q into a store filter for the page, resolving @me to the viewer.
func listStoreFilter(viewer *middleware.Claims, q listquery.Query, state string, page int) store.ListFilter {
	f := store.ListFilter{
		Text:        q.Text,
		Author:      q.Author,
		Assignee:    q.Assignee,
		Labels:      q.Labels,
		Milestone:   q.Milestone,
		MilestoneID: q.MilestoneID,
		No:          q.No,
		Sort:        q.Sort,
		State:       state,
		Limit:       listPageSize,
		Offset:      (page - 1) * listPageSize,
	}
	for _, who := range []*string{&f.Author, &f.Assignee} {
		if !strings.EqualFold(*who, listquery.Me) {
			continue
		}
		if viewer != nil {
			*who = viewer.Username
		} else {
			f.NoMatch = true
		}
	}
	return f
}

// listMilestoneTitle lets a legacy ?milestone=<id> link show up as a milestone: chip.
func listMilestoneTitle(q listquery.Query, milestones []model.Milestone) listquery.Query {
	if q.MilestoneID == 0 {
		return q
	}
	for _, m := range milestones {
		if m.ID == q.MilestoneID {
			q.Milestone, q.MilestoneID = m.Title, 0
			break
		}
	}
	return q
}

// listFilterData builds the search field, menus and chips for the list at base. q has its is: set to the active tab.
func listFilterData(base, noun string, q listquery.Query, total int, people []string, signedIn bool, labels []model.Label, milestones []model.Milestone) components.ListFilterData {
	link := func(q listquery.Query) string { return listURL(base, q, 1) }
	d := components.ListFilterData{
		Action:      base,
		Placeholder: "Search or filter, e.g. is:open label:bug author:@me",
		Text:        q.Text,
		SearchBase:  q.Without(listquery.KeyText, "").String(),
		ClearHref:   link(listquery.Query{State: q.State}),
		HasFilters:  q.HasFilters(),
		Total:       total,
		Noun:        noun,
		Chips:       listChips(link, q),
	}

	// strip removes what the menu sets, so the form's f carries everything else and unchecking all clears the menu.
	menu := func(title string, multi bool, strip func(listquery.Query) listquery.Query, opts []components.ListFilterOption) components.ListFilterMenu {
		m := components.ListFilterMenu{Title: title, Multi: multi, Base: strip(q).String(), Options: opts}
		if multi {
			for _, o := range opts {
				if o.Checked {
					m.Selected++
				}
			}
		}
		return m
	}
	opt := func(key, value, label string) components.ListFilterOption {
		return components.ListFilterOption{Token: listquery.Token(key, value), Label: label, Checked: q.Has(key, value)}
	}
	none := func(key, label string, divider bool) components.ListFilterOption {
		o := opt(listquery.KeyNo, key, label)
		o.Divider = divider
		return o
	}
	names := people
	if signedIn {
		names = append([]string{listquery.Me}, people...)
	}
	person := func(key string) []components.ListFilterOption {
		opts := make([]components.ListFilterOption, 0, len(names)+1)
		for _, n := range names {
			opts = append(opts, opt(key, n, n))
		}
		return opts
	}

	d.Menus = append(d.Menus, menu("Author", false, func(q listquery.Query) listquery.Query {
		return q.Without(listquery.KeyAuthor, "")
	}, person(listquery.KeyAuthor)))

	var labelOpts []components.ListFilterOption
	for _, l := range labels {
		o := opt(listquery.KeyLabel, l.Name, l.Name)
		o.Color = l.Color
		labelOpts = append(labelOpts, o)
	}
	labelOpts = append(labelOpts, none("label", "No label", len(labelOpts) > 0))
	d.Menus = append(d.Menus, menu("Label", true, func(q listquery.Query) listquery.Query {
		for _, l := range q.Labels {
			q = q.Without(listquery.KeyLabel, l)
		}
		return q.Without(listquery.KeyNo, "label")
	}, labelOpts))

	var msOpts []components.ListFilterOption
	for _, m := range milestones {
		msOpts = append(msOpts, opt(listquery.KeyMilestone, m.Title, m.Title))
	}
	msOpts = append(msOpts, none("milestone", "No milestone", len(msOpts) > 0))
	d.Menus = append(d.Menus, menu("Milestone", false, func(q listquery.Query) listquery.Query {
		return q.Without(listquery.KeyMilestone, "").Without(listquery.KeyNo, "milestone")
	}, msOpts))

	assigneeOpts := append(person(listquery.KeyAssignee), none("assignee", "Assigned to nobody", len(names) > 0))
	d.Menus = append(d.Menus, menu("Assignee", false, func(q listquery.Query) listquery.Query {
		return q.Without(listquery.KeyAssignee, "").Without(listquery.KeyNo, "assignee")
	}, assigneeOpts))

	var sortOpts []components.ListFilterOption
	for _, s := range []struct{ value, label string }{
		{"newest", "Newest"}, {"oldest", "Oldest"}, {"updated", "Recently updated"}, {"comments", "Most commented"},
	} {
		o := opt(listquery.KeySort, s.value, s.label)
		o.Checked = q.Sort == s.value || q.Sort == "" && s.value == "newest"
		sortOpts = append(sortOpts, o)
	}
	d.Menus = append(d.Menus, menu("Sort", false, func(q listquery.Query) listquery.Query {
		return q.Without(listquery.KeySort, "")
	}, sortOpts))
	return d
}

// listChips has one removable chip per filter qualifier. The state shows as the active tab, not as a chip.
func listChips(link func(listquery.Query) string, q listquery.Query) []components.ListFilterChip {
	var chips []components.ListFilterChip
	add := func(key, value string) {
		chips = append(chips, components.ListFilterChip{Label: listquery.Token(key, value), RemoveHref: link(q.Without(key, value))})
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
	return chips
}
