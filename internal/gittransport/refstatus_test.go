package gittransport_test

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func TestCountRefStatus(t *testing.T) {
	if ok, failed := gittransport.CountRefStatus(nil); ok != 0 || failed != 0 {
		t.Fatalf("nil report: want (0,0), got (%d,%d)", ok, failed)
	}

	status := &packp.ReportStatus{
		CommandStatuses: []*packp.CommandStatus{
			{ReferenceName: "refs/heads/main", Status: "ok"},
			{ReferenceName: "refs/heads/dev", Status: "non-fast-forward"},
			{ReferenceName: "refs/heads/release", Status: "ok"},
		},
	}
	ok, failed := gittransport.CountRefStatus(status)
	if ok != 2 || failed != 1 {
		t.Fatalf("want (ok=2, failed=1), got (ok=%d, failed=%d)", ok, failed)
	}
}

func TestAppliedCommands(t *testing.T) {
	commands := []*packp.Command{
		{Name: "refs/heads/main"},
		{Name: "refs/heads/dev"},
		{Name: "refs/heads/release"},
	}

	if got := gittransport.AppliedCommands(nil, commands); len(got) != 3 {
		t.Errorf("nil report: want all 3 commands, got %d", len(got))
	}

	allOK := &packp.ReportStatus{CommandStatuses: []*packp.CommandStatus{
		{ReferenceName: "refs/heads/main", Status: "ok"},
		{ReferenceName: "refs/heads/dev", Status: "ok"},
		{ReferenceName: "refs/heads/release", Status: "ok"},
	}}
	if got := gittransport.AppliedCommands(allOK, commands); len(got) != 3 {
		t.Errorf("all-ok report: want all 3 commands, got %d", len(got))
	}

	mixed := &packp.ReportStatus{CommandStatuses: []*packp.CommandStatus{
		{ReferenceName: "refs/heads/main", Status: "ok"},
		{ReferenceName: "refs/heads/dev", Status: "non-fast-forward"},
		{ReferenceName: "refs/heads/release", Status: "ok"},
	}}
	got := gittransport.AppliedCommands(mixed, commands)
	if len(got) != 2 {
		t.Fatalf("mixed report: want 2 applied commands, got %d", len(got))
	}
	for _, c := range got {
		if c.Name == "refs/heads/dev" {
			t.Error("the failed ref refs/heads/dev must be excluded")
		}
	}
}
