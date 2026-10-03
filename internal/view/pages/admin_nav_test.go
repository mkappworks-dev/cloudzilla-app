package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestAdminPages_LinkEachOther(t *testing.T) {
	adminPages := map[string]templ.Component{
		"/admin/settings":  pages.AdminSettings(view.AdminSettingsData{}),
		"/admin/audit-log": pages.AuditLog(view.AuditLogData{Page: 1, PerPage: 50}),
		"/admin/sso":       pages.SSOSettings(view.SSOSettingsData{}),
	}
	for path, page := range adminPages {
		t.Run(path, func(t *testing.T) {
			var sb strings.Builder
			if err := page.Render(context.Background(), &sb); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := sb.String()
			for other := range adminPages {
				if !strings.Contains(out, `href="`+other+`"`) {
					t.Errorf("no link to %s", other)
				}
			}
			if !strings.Contains(out, `href="`+path+`" aria-current="page"`) {
				t.Errorf("tab for %s is not marked current", path)
			}
		})
	}
}
