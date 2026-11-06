package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/listquery"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestRedirectListForm(t *testing.T) {
	tests := []struct {
		name, url, want string
	}{
		{"plain list is left alone", "/o/r/issues?q=is%3Aclosed", ""},
		{"search merges base and typed text", "/o/r/issues?f=is%3Aclosed+label%3Abug&q=crash+author%3Apriya", "/o/r/issues?q=is%3Aclosed+author%3Apriya+label%3Abug+crash"},
		{"menu tokens are added to the base", "/o/r/issues?f=is%3Aclosed&v=label%3Aui&v=label%3Abug", "/o/r/issues?q=is%3Aclosed+label%3Abug+label%3Aui"},
		{"single-select replaces", "/o/r/issues?f=&v=sort%3Aoldest", "/o/r/issues?q=sort%3Aoldest"},
		{"emptying every control clears the query", "/o/r/issues?f=&q=", "/o/r/issues"},
		{"quoted labels survive", "/o/r/issues?f=&v=label%3A%22good+first+issue%22", "/o/r/issues?q=label%3A%22good+first+issue%22"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			redirected := redirectListForm(w, httptest.NewRequest(http.MethodGet, tt.url, nil), "/o/r/issues")
			if redirected != (tt.want != "") {
				t.Fatalf("redirected = %v, want %v", redirected, tt.want != "")
			}
			if tt.want == "" {
				return
			}
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tt.want {
				t.Errorf("got %d %q, want 303 %q", w.Code, w.Header().Get("Location"), tt.want)
			}
		})
	}
}

func TestRedirectListFormIgnoresRequestPath(t *testing.T) {
	w := httptest.NewRecorder()
	redirectListForm(w, httptest.NewRequest(http.MethodGet, "//evil.example/x?f=&q=a", nil), "/o/r/issues")
	if got := w.Header().Get("Location"); got != "/o/r/issues?q=a" {
		t.Errorf("Location = %q, want a path under the route base", got)
	}
}

func TestListURL(t *testing.T) {
	q := listquery.Parse("label:bug crash")
	if got, want := listURL("/o/r/pulls", q, 1), "/o/r/pulls?q=label%3Abug+crash"; got != want {
		t.Errorf("page 1 = %q, want %q", got, want)
	}
	if got, want := listURL("/o/r/pulls", q, 3), "/o/r/pulls?page=3&q=label%3Abug+crash"; got != want {
		t.Errorf("page 3 = %q, want %q", got, want)
	}
	if got, want := listURL("/o/r/pulls", listquery.Query{}, 1), "/o/r/pulls"; got != want {
		t.Errorf("empty = %q, want %q", got, want)
	}
}

func TestListTotalPages(t *testing.T) {
	for total, want := range map[int]int{0: 1, 1: 1, 25: 1, 26: 2, 50: 2, 51: 3} {
		if got := listTotalPages(total); got != want {
			t.Errorf("listTotalPages(%d) = %d, want %d", total, got, want)
		}
	}
}

func TestListStoreFilterResolvesMe(t *testing.T) {
	q := listquery.Parse("author:@me assignee:priya")

	f := listStoreFilter(&middleware.Claims{UserID: 7, Username: "ada"}, q, "open", 3)
	if f.Author != "ada" || f.Assignee != "priya" || f.NoMatch {
		t.Errorf("signed in: author=%q assignee=%q noMatch=%v", f.Author, f.Assignee, f.NoMatch)
	}
	if f.Limit != listPageSize || f.Offset != 2*listPageSize {
		t.Errorf("paging: limit=%d offset=%d", f.Limit, f.Offset)
	}

	if f := listStoreFilter(nil, q, "open", 1); !f.NoMatch {
		t.Error("signed out: @me must match nothing")
	}
}

func TestListFilterDataPlaceholderOnlyAdvertisesParsedQualifiers(t *testing.T) {
	d := listFilterData("/o/r/issues", "issue", listquery.Query{}, 0, nil, false, nil, nil)
	example := strings.TrimPrefix(d.Placeholder, "Search or filter, e.g. ")
	q := listquery.Parse(example)
	if q.Text != "" || q.State != "open" || !q.Has(listquery.KeyLabel, "bug") || q.Author != listquery.Me {
		t.Errorf("placeholder example %q does not parse into qualifiers: %+v", example, q)
	}
}

func TestListFilterDataChipsAndMenus(t *testing.T) {
	q := listquery.Parse("label:bug label:ui author:priya no:milestone sort:updated crash")
	labels := []model.Label{{Name: "bug", Color: "#f00"}, {Name: "ui", Color: "#00f"}, {Name: "docs", Color: "#0f0"}}
	d := listFilterData("/o/r/issues", "issue", q, 4, []string{"priya", "ada"}, true, labels, []model.Milestone{{Title: "v1"}})

	if !d.HasFilters || d.Total != 4 || d.Text != "crash" || d.ClearHref != "/o/r/issues" {
		t.Errorf("summary: %+v", d)
	}
	var chips []string
	for _, c := range d.Chips {
		chips = append(chips, c.Label)
	}
	if want := []string{"author:priya", "label:bug", "label:ui", "no:milestone", "sort:updated"}; !reflect.DeepEqual(chips, want) {
		t.Errorf("chips = %v, want %v", chips, want)
	}
	if got := d.Chips[1].RemoveHref; got != "/o/r/issues?q=author%3Apriya+no%3Amilestone+label%3Aui+sort%3Aupdated+crash" &&
		got != "/o/r/issues?q=author%3Apriya+label%3Aui+no%3Amilestone+sort%3Aupdated+crash" {
		t.Errorf("remove label:bug href = %q", got)
	}
	if d.SearchBase != "author:priya label:bug label:ui no:milestone sort:updated" {
		t.Errorf("SearchBase = %q", d.SearchBase)
	}

	menus := map[string]int{}
	for _, m := range d.Menus {
		menus[m.Title] = m.Selected
	}
	if menus["Label"] != 2 {
		t.Errorf("Label menu selected = %d, want 2", menus["Label"])
	}
	for _, m := range d.Menus {
		if m.Title == "Label" {
			if strings.Contains(m.Base, "label:") || m.Base != "author:priya no:milestone sort:updated crash" {
				t.Errorf("Label menu base = %q", m.Base)
			}
		}
		if m.Title == "Milestone" && !m.Options[len(m.Options)-1].Checked {
			t.Errorf("No milestone should be checked: %+v", m.Options)
		}
		if m.Title == "Author" && (m.Options[0].Label != listquery.Me || m.Options[1].Label != "priya" || !m.Options[1].Checked) {
			t.Errorf("Author options: %+v", m.Options)
		}
	}
}

func TestListMilestoneTitle(t *testing.T) {
	q := listquery.FromValues(map[string][]string{"milestone": {"12"}})
	got := listMilestoneTitle(q, []model.Milestone{{ID: 12, Title: "v0.3"}})
	if got.Milestone != "v0.3" || got.MilestoneID != 0 {
		t.Errorf("resolved = %+v", got)
	}
	if got := listMilestoneTitle(q, nil); got.Milestone != "12" || got.MilestoneID != 0 {
		t.Errorf("unknown ID should become a visible milestone that matches nothing: %+v", got)
	}
}
