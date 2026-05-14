package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// activeTabLabel returns the label text rendered between the first
// aria-current="page" and the closing </a>. Empty string if no active tab.
func activeTabLabel(out string) string {
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

func TestRepoSubnav_RendersAllTabsWithHrefs(t *testing.T) {
	data := RepoSubnavData{
		OwnerName: "alice",
		RepoName:  "demo",
		Active:    "code",
		CanManage: true,
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	cases := []struct {
		label, href string
	}{
		{"Code", "/alice/demo"},
		{"Issues", "/alice/demo/issues"},
		{"Pull requests", "/alice/demo/pulls"},
		{"Discussions", "/alice/demo/discussions"},
		{"Projects", "/alice/demo/projects"},
		{"Wiki", "/alice/demo/wiki"},
		{"Releases", "/alice/demo/releases"},
		{"Settings", "/alice/demo/settings"},
	}
	for _, c := range cases {
		if !strings.Contains(out, c.label) {
			t.Errorf("expected label %q in output", c.label)
		}
		if !strings.Contains(out, `href="`+c.href+`"`) {
			t.Errorf("expected href=%q for tab %q; output: %s", c.href, c.label, out)
		}
	}
}

func TestRepoSubnav_MarksOnlyActiveTab(t *testing.T) {
	data := RepoSubnavData{OwnerName: "alice", RepoName: "demo", Active: "pull_requests"}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if got := strings.Count(out, `aria-current="page"`); got != 1 {
		t.Fatalf("expected exactly 1 active tab, got %d; output: %s", got, out)
	}
	active := activeTabLabel(out)
	if !strings.Contains(active, "Pull requests") {
		t.Errorf("expected active tab to contain 'Pull requests'; active region: %q", active)
	}
	for _, wrong := range []string{"Code", "Issues", "Discussions", "Projects", "Wiki", "Releases", "Settings"} {
		if strings.Contains(active, wrong) {
			t.Errorf("active region should not contain %q; got: %q", wrong, active)
		}
	}
}

func TestRepoSubnav_RendersCountBadges(t *testing.T) {
	data := RepoSubnavData{
		OwnerName: "alice",
		RepoName:  "demo",
		Active:    "code",
		Counts: map[string]int{
			"issues":        23,
			"pull_requests": 12,
			"discussions":   5,
			"releases":      3,
		},
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, n := range []string{"23", "12", "5", "3"} {
		if !strings.Contains(out, ">"+n+"<") {
			t.Errorf("expected count badge %q in output", n)
		}
	}
}

// badgeSpanMarker is a substring unique to the count-badge <span>.
// If it appears at all, at least one badge was rendered.
const badgeSpanMarker = `class="font-mono text-[11px] text-muted-foreground/70 ml-0.5"`

func TestRepoSubnav_OmitsZeroCount(t *testing.T) {
	data := RepoSubnavData{
		OwnerName: "alice",
		RepoName:  "demo",
		Active:    "code",
		Counts:    map[string]int{"issues": 0},
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, badgeSpanMarker) {
		t.Errorf("zero count must not render a badge span; output: %s", out)
	}
	if strings.Contains(out, ">0<") {
		t.Errorf("zero-count digit leaked into output; output: %s", out)
	}
}

func TestRepoSubnav_OmitsMissingCount(t *testing.T) {
	// Counts map present but key absent — must render no badge for that tab.
	data := RepoSubnavData{
		OwnerName: "alice",
		RepoName:  "demo",
		Active:    "code",
		Counts:    map[string]int{"pull_requests": 7},
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	// Exactly one badge (the pull_requests one) — every other tab key is missing.
	if got := strings.Count(out, badgeSpanMarker); got != 1 {
		t.Errorf("expected exactly 1 badge span (pull_requests=7), got %d; output: %s", got, out)
	}
}

func TestRepoSubnav_NilCountsMapRendersNoBadges(t *testing.T) {
	data := RepoSubnavData{OwnerName: "alice", RepoName: "demo", Active: "code"}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render with nil Counts: %v", err)
	}
	if strings.Contains(buf.String(), badgeSpanMarker) {
		t.Errorf("nil Counts must render zero badges; output: %s", buf.String())
	}
}

func TestRepoSubnav_HidesSettingsWhenCannotManage(t *testing.T) {
	data := RepoSubnavData{OwnerName: "alice", RepoName: "demo", Active: "code"} // CanManage defaults to false
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Settings") {
		t.Errorf("Settings tab must not render when CanManage is false; output: %s", out)
	}
	if strings.Contains(out, "/alice/demo/settings") {
		t.Errorf("Settings href must not render when CanManage is false; output: %s", out)
	}
}

func TestRepoSubnav_ShowsSettingsWhenCanManage(t *testing.T) {
	data := RepoSubnavData{OwnerName: "alice", RepoName: "demo", Active: "code", CanManage: true}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Settings") {
		t.Errorf("Settings tab must render when CanManage is true; output: %s", out)
	}
	if !strings.Contains(out, `href="/alice/demo/settings"`) {
		t.Errorf("Settings href must render when CanManage is true; output: %s", out)
	}
}
