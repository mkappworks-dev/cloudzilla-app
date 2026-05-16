package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestBulkActionsBar_RendersNothingWhenZeroSelected(t *testing.T) {
	d := BulkActionsBarData{
		SelectedCount: 0,
		Actions: []BulkAction{
			{Label: "Close", Href: "/close", Kind: "destructive"},
		},
	}
	var buf bytes.Buffer
	if err := BulkActionsBar(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output when SelectedCount=0, got: %s", buf.String())
	}
}

func TestBulkActionsBar_RendersCountAndActions(t *testing.T) {
	d := BulkActionsBarData{
		SelectedCount: 3,
		Actions: []BulkAction{
			{Label: "Mark as read", Href: "/mark-read", Kind: ""},
			{Label: "Assign", Href: "/assign", Kind: "primary"},
			{Label: "Delete", Href: "/delete", Kind: "destructive"},
		},
	}
	var buf bytes.Buffer
	if err := BulkActionsBar(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "3 selected") {
		t.Errorf("expected '3 selected' in output, got: %s", out)
	}
	for _, want := range []string{"Mark as read", "Assign", "Delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected action label %q in output, got: %s", want, out)
		}
	}
	if !strings.Contains(out, "text-destructive") {
		t.Errorf("expected destructive class in output, got: %s", out)
	}
	if !strings.Contains(out, "bg-primary") {
		t.Errorf("expected primary class in output, got: %s", out)
	}
	if !strings.Contains(out, `role="region"`) {
		t.Errorf("expected role=region in output, got: %s", out)
	}
}
