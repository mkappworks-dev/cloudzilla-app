package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestQuotaUsageLine(t *testing.T) {
	const summary = "Repositories 12 of 50 · Storage 1.2 GiB of 10 GiB"
	pagesUnderTest := map[string]func(summary string) templ.Component{
		"settings": func(s string) templ.Component {
			return pages.Settings(view.SettingsData{User: model.User{ID: 1, Username: "alice"}, QuotaSummary: s})
		},
		"org settings": func(s string) templ.Component {
			return pages.OrgSettings(view.OrgSettingsData{Org: model.Organization{Name: "acme"}, QuotaSummary: s})
		},
	}
	for name, page := range pagesUnderTest {
		t.Run(name+" shows the line when a quota is set", func(t *testing.T) {
			var sb strings.Builder
			if err := page(summary).Render(context.Background(), &sb); err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(sb.String(), `id="quota-usage"`) || !strings.Contains(sb.String(), summary) {
				t.Errorf("the page doesn't show %q", summary)
			}
		})
		t.Run(name+" shows nothing when no quota is set", func(t *testing.T) {
			var sb strings.Builder
			if err := page("").Render(context.Background(), &sb); err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(sb.String(), "quota-usage") {
				t.Error("the page shows a quota line although no quota is set")
			}
		})
	}
}
