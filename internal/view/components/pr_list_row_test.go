package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPRListRow_RendersHrefAndMeta(t *testing.T) {
	d := PRListRowData{
		OwnerName: "alice", RepoName: "myrepo",
		Number:   7,
		Title:    "Add dark mode",
		Author:   "bob",
		State:    "open",
		CIStatus: "success",
		OpenedAt: "2 days ago",
	}
	var buf bytes.Buffer
	if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "/alice/myrepo/pulls/7") {
		t.Errorf("expected href with /alice/myrepo/pulls/7, got: %s", out)
	}
	if !strings.Contains(out, "Add dark mode") {
		t.Errorf("expected title in output, got: %s", out)
	}
	if !strings.Contains(out, "#7 opened 2 days ago by bob") {
		t.Errorf("expected meta line in output, got: %s", out)
	}
}

func TestPRListRow_StateIconAriaLabel(t *testing.T) {
	cases := []struct {
		state   string
		wantAria string
	}{
		{"open", `aria-label="open"`},
		{"draft", `aria-label="draft"`},
		{"merged", `aria-label="merged"`},
		{"closed", `aria-label="closed"`},
	}
	for _, tc := range cases {
		d := PRListRowData{
			OwnerName: "o", RepoName: "r", Number: 1,
			Title: "t", Author: "a", State: tc.state, OpenedAt: "now",
		}
		var buf bytes.Buffer
		if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
			t.Fatalf("state=%s render: %v", tc.state, err)
		}
		if !strings.Contains(buf.String(), tc.wantAria) {
			t.Errorf("state=%s: expected %q in output, got: %s", tc.state, tc.wantAria, buf.String())
		}
	}
}

func TestPRListRow_RendersLabelChips(t *testing.T) {
	d := PRListRowData{
		OwnerName: "o", RepoName: "r", Number: 1,
		Title: "t", Author: "a", State: "open", OpenedAt: "now",
		LabelChips: []LabelChip{
			{Name: "bug", Color: "#d73a4a"},
			{Name: "enhancement", Color: "#84b6eb"},
		},
	}
	var buf bytes.Buffer
	if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"bug", "enhancement", "#d73a4a", "#84b6eb"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestPRListRow_CIBadgeAriaLabel(t *testing.T) {
	cases := []struct {
		ciStatus string
		wantAria string
	}{
		{"success", `aria-label="CI passing"`},
		{"failure", `aria-label="CI failing"`},
		{"error", `aria-label="CI failing"`},
		{"pending", `aria-label="CI pending"`},
	}
	for _, tc := range cases {
		d := PRListRowData{
			OwnerName: "o", RepoName: "r", Number: 1,
			Title: "t", Author: "a", State: "open", OpenedAt: "now",
			CIStatus: tc.ciStatus,
		}
		var buf bytes.Buffer
		if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
			t.Fatalf("ciStatus=%s render: %v", tc.ciStatus, err)
		}
		if !strings.Contains(buf.String(), tc.wantAria) {
			t.Errorf("ciStatus=%s: expected %q in output, got: %s", tc.ciStatus, tc.wantAria, buf.String())
		}
	}
}

func TestPRListRow_EmptyCIStatus_NoBadge(t *testing.T) {
	d := PRListRowData{
		OwnerName: "o", RepoName: "r", Number: 1,
		Title: "t", Author: "a", State: "open", OpenedAt: "now",
		CIStatus: "",
	}
	var buf bytes.Buffer
	if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, marker := range []string{`aria-label="CI passing"`, `aria-label="CI failing"`, `aria-label="CI pending"`} {
		if strings.Contains(out, marker) {
			t.Errorf("CIStatus empty: expected no CI badge, but found %q in: %s", marker, out)
		}
	}
}
