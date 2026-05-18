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
	for _, want := range []string{"Add dark mode", "#7", "bob", "2 days ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestPRListRow_StateBadge(t *testing.T) {
	cases := []struct {
		state    string
		wantText string
	}{
		{"open", "Open"},
		{"draft", "Draft"},
		{"merged", "Merged"},
		{"closed", "Closed"},
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
		if !strings.Contains(buf.String(), tc.wantText) {
			t.Errorf("state=%s: expected badge text %q in output, got: %s", tc.state, tc.wantText, buf.String())
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

func TestPRListRow_CIBadge(t *testing.T) {
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
			CIStatus: tc.ciStatus, CIPassing: 8, CITotal: 12,
		}
		var buf bytes.Buffer
		if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
			t.Fatalf("ciStatus=%s render: %v", tc.ciStatus, err)
		}
		out := buf.String()
		if !strings.Contains(out, tc.wantAria) {
			t.Errorf("ciStatus=%s: expected %q in output, got: %s", tc.ciStatus, tc.wantAria, out)
		}
		if !strings.Contains(out, "8/12") {
			t.Errorf("ciStatus=%s: expected check count 8/12 in output, got: %s", tc.ciStatus, out)
		}
	}
}

func TestPRListRow_NoChecks_NoCIBadge(t *testing.T) {
	d := PRListRowData{
		OwnerName: "o", RepoName: "r", Number: 1,
		Title: "t", Author: "a", State: "open", OpenedAt: "now",
		CIStatus: "success", CITotal: 0,
	}
	var buf bytes.Buffer
	if err := PRListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, marker := range []string{`aria-label="CI passing"`, `aria-label="CI failing"`, `aria-label="CI pending"`} {
		if strings.Contains(out, marker) {
			t.Errorf("CITotal 0: expected no CI badge, but found %q in: %s", marker, out)
		}
	}
}

func TestPRListRow_CommentCount(t *testing.T) {
	withComments := PRListRowData{
		OwnerName: "o", RepoName: "r", Number: 1,
		Title: "t", Author: "a", State: "open", OpenedAt: "now",
		CommentCount: 5,
	}
	var buf bytes.Buffer
	if err := PRListRow(withComments).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "5 comments") {
		t.Errorf("expected comment count title in output, got: %s", buf.String())
	}

	noComments := withComments
	noComments.CommentCount = 0
	buf.Reset()
	if err := PRListRow(noComments).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), "comments") {
		t.Errorf("CommentCount 0: expected no comment indicator, got: %s", buf.String())
	}
}
