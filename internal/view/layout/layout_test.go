package layout_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mkappworks-dev/cloudzilla-app/internal/assets"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

func TestBase_ReferencesAssetsByContentHash(t *testing.T) {
	set, err := assets.New(fstest.MapFS{
		"static/favicon.svg":     {Data: []byte("<svg/>")},
		"static/main.css":        {Data: []byte("body{color:red}")},
		"static/code-themes.css": {Data: []byte(".hl{color:blue}")},
		"static/mermaid.min.js":  {Data: []byte("mermaid")},
		"htmx.min.js":            {Data: []byte("htmx")},
		"alpine.min.js":          {Data: []byte("alpine")},
	})
	if err != nil {
		t.Fatalf("assets.New: %v", err)
	}
	assets.SetDefault(set)
	t.Cleanup(func() { assets.SetDefault(nil) })

	var page strings.Builder
	if err := layout.Base(view.BasePage{}, "Home").Render(context.Background(), &page); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`href="/static/favicon.svg?v=d4dc56669143034f"`,
		`href="/static/main.css?v=15c42ab7768d955e"`,
		`href="/static/code-themes.css?v=b276a8a300dfde39"`,
		`src="/htmx.min.js?v=dc476210dea6474d"`,
		`src="/alpine.min.js?v=54c5b3dd459d5ef7"`,
		`data-mermaid-src="/static/mermaid.min.js?v=0fbccedd61528383"`,
	} {
		if !strings.Contains(page.String(), want) {
			t.Errorf("layout lacks %s", want)
		}
	}
}

func TestBase_UserMenuLinksAdminForSuperadminOnly(t *testing.T) {
	const adminMenuItem = `href="/admin/settings" role="menuitem"`
	for _, tc := range []struct {
		name       string
		superadmin bool
	}{
		{"superadmin", true},
		{"user", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := view.BasePage{CurrentUser: &middleware.Claims{Username: "alice", IsSuperadmin: tc.superadmin}}
			var page strings.Builder
			if err := layout.Base(base, "Home").Render(context.Background(), &page); err != nil {
				t.Fatalf("render: %v", err)
			}
			if got := strings.Contains(page.String(), adminMenuItem); got != tc.superadmin {
				t.Errorf("user menu links admin = %v, want %v", got, tc.superadmin)
			}
		})
	}
}

func TestBase_HelpLinksPointUpstream(t *testing.T) {
	base := view.BasePage{CurrentUser: &middleware.Claims{Username: "alice"}}
	var page strings.Builder
	if err := layout.Base(base, "Home").Render(context.Background(), &page); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := page.String()
	for _, dead := range []string{`href="/docs"`, `href="/api"`, `href="/changelog"`, `href="/status"`} {
		if strings.Contains(html, dead) {
			t.Errorf("layout links %s, which has no route", dead)
		}
	}
	for _, want := range []string{
		`href="https://github.com/mkappworks-dev/cloudzilla-app/tree/main/docs"`,
		`href="https://github.com/mkappworks-dev/cloudzilla-app/blob/main/docs/api-reference.md"`,
		`href="https://github.com/mkappworks-dev/cloudzilla-app/blob/main/CHANGELOG.md"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("layout lacks %s", want)
		}
	}
}

func TestBase_FooterShowsBuildVersion(t *testing.T) {
	prev := view.Version()
	view.SetVersion("v9.9.9")
	t.Cleanup(func() { view.SetVersion(prev) })

	var page strings.Builder
	if err := layout.Base(view.BasePage{}, "Home").Render(context.Background(), &page); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(page.String(), "cloudzilla / v9.9.9 <span") {
		t.Errorf("footer lacks the build version v9.9.9")
	}
}

func TestBase_SwitchersCapEntriesAndLinkToFullList(t *testing.T) {
	base := view.BasePage{
		CurrentUser: &middleware.Claims{Username: "alice"},
		RepoSubnav:  &view.RepoSubnavInfo{OwnerName: "alice", RepoName: "r0"},
	}
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("org%02d", i)
		base.UserOrgs = append(base.UserOrgs, view.OrgEntry{Org: model.Organization{Name: name}, Role: model.OrgRoleMember})
		repo := fmt.Sprintf("r%02d", i)
		base.RepoSwitcher = append(base.RepoSwitcher, view.RepoRef{Name: repo, Path: "/alice/" + repo})
	}
	var page strings.Builder
	if err := layout.Base(base, "Home").Render(context.Background(), &page); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := page.String()
	for _, tc := range []struct{ present, absent string }{
		{`href="/org07"`, `href="/org08"`},
		{`href="/alice/r07"`, `href="/alice/r08"`},
	} {
		if !strings.Contains(html, tc.present) || strings.Contains(html, tc.absent) {
			t.Errorf("want %s listed and %s capped", tc.present, tc.absent)
		}
	}
	for _, want := range []string{"View all 12 organizations", "View all 12 repositories", `href="/alice?tab=repositories"`} {
		if !strings.Contains(html, want) {
			t.Errorf("layout lacks %q", want)
		}
	}
}
