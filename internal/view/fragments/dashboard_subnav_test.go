package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDashboardSubnav_RendersAllTabs(t *testing.T) {
	data := DashboardSubnavData{Username: "alice", Active: "overview"}
	var buf bytes.Buffer
	if err := DashboardSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, tab := range []string{"Overview", "Repositories", "Projects", "Packages", "Stars", "Pulls", "Issues", "Activity"} {
		if !strings.Contains(out, tab) {
			t.Errorf("expected %q in output", tab)
		}
	}
}
