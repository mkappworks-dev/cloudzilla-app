package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// activeDashTabLabel returns the label rendered between aria-current="page"
// and the first </a> after it.
func activeDashTabLabel(out string) string {
	idx := strings.Index(out, `aria-current="page"`)
	if idx < 0 {
		return ""
	}
	slice := out[idx:]
	end := strings.Index(slice, "</a>")
	if end < 0 {
		return ""
	}
	return slice[:end]
}

func TestDashboardSubnav_RendersAllTabsWithHrefs(t *testing.T) {
	data := DashboardSubnavData{Username: "alice", Active: "overview"}
	var buf bytes.Buffer
	if err := DashboardSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	cases := []struct {
		label, href string
	}{
		{"Overview", "/alice"},
		{"Repositories", "/alice?tab=repositories"},
		{"Projects", "/alice?tab=projects"},
		{"Packages", "/alice?tab=packages"},
		{"Stars", "/stars"},
		{"Pulls", "/pulls"},
		{"Issues", "/issues"},
		{"Activity", "/activity"},
	}
	for _, c := range cases {
		if !strings.Contains(out, c.label) {
			t.Errorf("expected label %q in output", c.label)
		}
		// Templ encodes `?` as `&#63;` in href attribute values.
		want := strings.ReplaceAll(c.href, "?", "&#63;")
		if !strings.Contains(out, `href="`+want+`"`) && !strings.Contains(out, `href="`+c.href+`"`) {
			t.Errorf("expected href for %q to be %q; output: %s", c.label, c.href, out)
		}
	}
}

func TestDashboardSubnav_MarksOnlyActiveTab(t *testing.T) {
	data := DashboardSubnavData{Username: "alice", Active: "repositories"}
	var buf bytes.Buffer
	if err := DashboardSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if got := strings.Count(out, `aria-current="page"`); got != 1 {
		t.Fatalf("expected exactly 1 active tab, got %d; output: %s", got, out)
	}
	active := activeDashTabLabel(out)
	if !strings.Contains(active, "Repositories") {
		t.Errorf("expected active region to contain 'Repositories'; got: %q", active)
	}
	for _, wrong := range []string{"Overview", "Projects", "Packages", "Stars", "Pulls", "Issues", "Activity"} {
		if strings.Contains(active, wrong) {
			t.Errorf("active region should not contain %q; got: %q", wrong, active)
		}
	}
}

func TestDashboardSubnav_NoActiveWhenEmpty(t *testing.T) {
	data := DashboardSubnavData{Username: "alice", Active: ""}
	var buf bytes.Buffer
	if err := DashboardSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), `aria-current="page"`) {
		t.Errorf("expected no aria-current when Active is empty; got: %s", buf.String())
	}
}
