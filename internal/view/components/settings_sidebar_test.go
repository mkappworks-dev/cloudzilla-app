package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSettingsSidebar(t *testing.T) {
	items := []SettingsSidebarItem{
		{Label: "General", Href: "/repo/settings", Key: "general"},
		{Label: "Access", Href: "/repo/settings/access", Key: "access"},
		{Label: "Webhooks", Href: "/repo/settings/webhooks", Key: "webhooks"},
		{Label: "Archive", Href: "/repo/settings/archive", Key: "archive", Danger: true, GroupLabel: "Danger zone"},
		{Label: "Delete repository", Href: "/repo/settings/delete", Key: "delete", Danger: true},
	}

	var buf bytes.Buffer
	if err := SettingsSidebar(SettingsSidebarData{Items: items, Active: "access"}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()

	// nav landmark
	if !strings.Contains(out, `aria-label="Settings sections"`) {
		t.Errorf("missing aria-label on nav")
	}

	// active item gets aria-current="page"
	if !strings.Contains(out, `aria-current="page"`) {
		t.Errorf("missing aria-current=page on active item")
	}

	// non-active items must NOT have aria-current
	idx := strings.Index(out, `aria-current="page"`)
	if idx == -1 {
		t.Fatalf("aria-current not found at all")
	}
	// strip the one occurrence and verify no second one remains
	remaining := out[:idx] + out[idx+len(`aria-current="page"`):]
	if strings.Contains(remaining, `aria-current`) {
		t.Errorf("aria-current present on more than one item")
	}

	// group label text appears
	if !strings.Contains(out, "Danger zone") {
		t.Errorf("missing GroupLabel text in output")
	}

	// all item labels appear
	for _, label := range []string{"General", "Access", "Webhooks", "Archive", "Delete repository"} {
		if !strings.Contains(out, label) {
			t.Errorf("missing label %q in output", label)
		}
	}

	// all hrefs appear
	for _, href := range []string{"/repo/settings", "/repo/settings/access", "/repo/settings/webhooks", "/repo/settings/archive", "/repo/settings/delete"} {
		if !strings.Contains(out, href) {
			t.Errorf("missing href %q in output", href)
		}
	}
}
