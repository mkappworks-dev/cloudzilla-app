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
