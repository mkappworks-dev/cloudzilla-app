package pages_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestAdminUsers_SortHeadersAndPaging(t *testing.T) {
	var sb strings.Builder
	data := view.AdminUsersData{
		Users:      []model.AdminUserRow{{User: model.User{Username: "ada", Email: "ada@example.test", CreatedAt: time.Now()}}},
		Filter:     model.AdminUserFilter{Query: "a", Sort: "username"},
		TotalCount: 120, Page: 2, PerPage: 50,
	}
	if err := pages.AdminUsers(data).Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{
		`href="/admin/users/ada"`,
		`aria-sort="ascending"`,
		// Clicking the sorted column flips it and returns to page 1; other columns start in their own direction.
		`href="/admin/users?dir=desc&amp;page=1&amp;q=a&amp;sort=username"`,
		`href="/admin/users?dir=desc&amp;page=1&amp;q=a&amp;sort=created"`,
		`href="/admin/users?dir=asc&amp;page=1&amp;q=a&amp;sort=email"`,
		// Paging keeps the sort.
		`href="/admin/users?dir=asc&amp;page=1&amp;q=a&amp;sort=username"`,
		`href="/admin/users?dir=asc&amp;page=3&amp;q=a&amp;sort=username"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Count(out, "aria-sort=") != 1 {
		t.Errorf("want exactly one aria-sort, got %d", strings.Count(out, "aria-sort="))
	}
}

func TestAuditLog_ShowsDetailsAndKeepsSortAcrossPages(t *testing.T) {
	var sb strings.Builder
	data := view.AuditLogData{
		Entries: []model.AuditEntry{
			{Action: "admin.user.suspend", CreatedAt: time.Now(), Metadata: map[string]any{"reason": "spam <b>", "tokens": float64(3)}},
			{Action: "login", CreatedAt: time.Now()},
		},
		Filter:     model.AuditFilter{Action: "login", Sort: "actor", Desc: true},
		TotalCount: 120, Page: 2, PerPage: 50,
	}
	if err := pages.AuditLog(data).Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{
		`>reason</dt>`, `spam &lt;b&gt;`, `>tokens</dt>`, `>3</dd>`,
		`aria-sort="descending"`,
		`href="/admin/audit-log?action=login&amp;dir=asc&amp;page=1&amp;sort=actor&amp;target_type="`,
		`href="/admin/audit-log?action=login&amp;dir=desc&amp;page=3&amp;sort=actor&amp;target_type="`,
		`href="/admin/audit-log?action=login&amp;dir=desc&amp;page=1&amp;sort=actor&amp;target_type="`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}
