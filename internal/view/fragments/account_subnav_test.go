package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAccountSubnav_RendersAllTabs(t *testing.T) {
	var buf bytes.Buffer
	if err := AccountSubnav(AccountSubnavData{Active: "overview"}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, label := range []string{"Overview", "Repositories", "Gists", "Pull requests", "Issues"} {
		if !strings.Contains(out, label) {
			t.Errorf("expected tab %q in output", label)
		}
	}
}

func TestAccountSubnav_MarksOnlyActiveTab(t *testing.T) {
	var buf bytes.Buffer
	if err := AccountSubnav(AccountSubnavData{Active: "pulls"}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := strings.Count(buf.String(), `aria-current="page"`); got != 1 {
		t.Fatalf("expected exactly 1 active tab, got %d", got)
	}
}

func TestAccountSubnav_RendersCountBadges(t *testing.T) {
	var buf bytes.Buffer
	data := AccountSubnavData{Active: "overview", Counts: map[string]int{"pulls": 12, "issues": 7}}
	if err := AccountSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, ">12<") || !strings.Contains(out, ">7<") {
		t.Errorf("expected count badges 12 and 7 in output")
	}
}

func TestAccountSubnav_OmitsZeroCount(t *testing.T) {
	var buf bytes.Buffer
	data := AccountSubnavData{Active: "overview", Counts: map[string]int{"pulls": 0}}
	if err := AccountSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), ">0<") {
		t.Errorf("zero count should not render a badge")
	}
}
