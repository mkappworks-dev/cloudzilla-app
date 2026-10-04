package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var siteNavMarkers = []string{
	`action="/search"`,
	`class="command-palette`,
	`aria-label="Footer"`,
}

// Until setup completes every route but /setup redirects back to it, so the
// setup page must not offer search, the command palette, or footer links.
func TestSetup_HidesSiteNav(t *testing.T) {
	var sb strings.Builder
	if err := pages.Setup(view.SetupData{}).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, m := range siteNavMarkers {
		if strings.Contains(out, m) {
			t.Errorf("setup page must not render %q", m)
		}
	}
}

func TestBase_RendersSiteNav(t *testing.T) {
	var sb strings.Builder
	if err := layout.Base(view.BasePage{}, "Explore").Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, m := range siteNavMarkers {
		if !strings.Contains(out, m) {
			t.Errorf("layout must render %q", m)
		}
	}
}
